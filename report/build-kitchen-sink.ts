import { readFileSync, writeFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';
import yaml from 'js-yaml';
import type { ConfigChange, ConfigSeverity } from './config-types.ts';
import type { KitchenSinkData } from './kitchen-sink/KitchenSinkTypes.ts';

type SchemaExample = Record<string, unknown> & { kind: string };

interface SchemaDefinition {
  examples?: unknown[];
}

interface SchemaDocument {
  $defs: Record<string, SchemaDefinition>;
}

const __dirname = dirname(fileURLToPath(import.meta.url));
const baseDataPath = resolve(__dirname, 'testdata/kitchen-sink.yaml');
// Schema examples are sourced from duty's generated change-types schema at compile time.
const schemaPath = resolve(__dirname, '../../duty/schema/openapi/change-types.schema.json');
const outputPath = resolve(__dirname, 'kitchen-sink.json');

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isSchemaExample(value: unknown): value is SchemaExample {
  return isRecord(value) && typeof value.kind === 'string';
}

function asText(value: unknown): string | undefined {
  if (typeof value === 'string') {
    const trimmed = value.trim();
    return trimmed || undefined;
  }
  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value);
  }
  return undefined;
}

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

function kindBase(kind: string): string {
  return kind.split('/')[0] ?? kind;
}

function statusText(example: SchemaExample): string {
  return asText(example.status)?.toLowerCase() ?? '';
}

function changeTypeForExample(example: SchemaExample): string {
  const kind = example.kind;
  const status = statusText(example);

  switch (kind) {
    case 'Approval/v1':
      if (status.includes('approved')) return 'Approved';
      if (status.includes('rejected')) return 'Rejected';
      return 'Approval';
    case 'Backup/v1':
      if (status.includes('fail') || status.includes('error')) return 'BackupFailed';
      if (status.includes('running') || status.includes('pending') || status.includes('started') || status.includes('progress')) {
        return 'BackupStarted';
      }
      if (status.includes('complete') || status.includes('success')) return 'BackupCompleted';
      return 'Backup';
    case 'Restore/v1':
      if (status.includes('complete') || status.includes('success')) return 'RestoreCompleted';
      return 'Restore';
    case 'Scale/v1':
      return 'Scaling';
    case 'ConfigChange/v1':
      return 'diff';
    default:
      return kindBase(kind);
  }
}

function severityForExample(example: SchemaExample): ConfigSeverity {
  const kind = example.kind;
  const status = statusText(example);

  if (kind === 'Backup/v1' || kind === 'Restore/v1' || kind === 'Test/v1') {
    if (status.includes('fail') || status.includes('error')) return 'high';
    if (status.includes('pending') || status.includes('running') || status.includes('started')) return 'low';
  }

  if (kind === 'Approval/v1') {
    if (status.includes('rejected')) return 'medium';
    if (status.includes('pending')) return 'low';
  }

  if (kind === 'Scale/v1' || kind === 'PermissionChange/v1' || kind === 'UserChange/v1') {
    return 'low';
  }

  return 'info';
}

function extractStandaloneExamples(schema: SchemaDocument): SchemaExample[] {
  const standalone: SchemaExample[] = [];
  const defs = schema.$defs ?? {};

  const rootExamples = defs.ConfigChangeDetailsSchema?.examples ?? [];
  for (const example of rootExamples) {
    if (isSchemaExample(example)) {
      standalone.push(clone(example));
    }
  }

  for (const [name, definition] of Object.entries(defs)) {
    if (name === 'ConfigChangeDetailsSchema') {
      continue;
    }

    for (const example of definition.examples ?? []) {
      if (isSchemaExample(example)) {
        standalone.push(clone(example));
      }
    }
  }

  return standalone;
}

function buildSchemaExampleChanges(schema: SchemaDocument): ConfigChange[] {
  const examples = extractStandaloneExamples(schema);
  const startTimestamp = Date.parse('2026-04-10T23:59:00Z');

  return examples.map((typedChange, index) => ({
    id: `schema-example-${String(index + 1).padStart(3, '0')}`,
    configID: 'schema-example-catalog',
    configName: 'Schema Example Catalog',
    configType: 'Schema::Example',
    changeType: changeTypeForExample(typedChange),
    severity: severityForExample(typedChange),
    source: 'schema-examples',
    createdBy: 'schema-generator',
    createdAt: new Date(startTimestamp - (index * 60_000)).toISOString(),
    count: 1,
    typedChange,
  }));
}

function compileKitchenSink(): KitchenSinkData {
  const baseData = yaml.load(readFileSync(baseDataPath, 'utf-8')) as KitchenSinkData;
  const schema = JSON.parse(readFileSync(schemaPath, 'utf-8')) as SchemaDocument;
  const schemaExampleChanges = buildSchemaExampleChanges(schema);

  return {
    ...baseData,
    changes: [...(baseData.changes ?? []), ...schemaExampleChanges],
  };
}

const compiled = compileKitchenSink();
writeFileSync(outputPath, JSON.stringify(compiled, null, 2) + '\n');

if (process.argv.includes('--stdout')) {
  process.stdout.write(JSON.stringify(compiled, null, 2));
}

import { readFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';
import yaml from 'js-yaml';
import type { ConfigReportData } from './config-types.ts';

const __dirname = dirname(fileURLToPath(import.meta.url));
const raw = readFileSync(resolve(__dirname, 'testdata/kitchen-sink.yaml'), 'utf-8');
const data = yaml.load(raw) as ConfigReportData;

export default data;

import { readFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';
import yaml from 'js-yaml';
import type { KitchenSinkData } from './kitchen-sink/KitchenSinkTypes.ts';

const __dirname = dirname(fileURLToPath(import.meta.url));
const raw = readFileSync(resolve(__dirname, 'testdata/kitchen-sink.yaml'), 'utf-8');
export const data = yaml.load(raw) as KitchenSinkData;

export default data;

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  process.stdout.write(JSON.stringify(data));
}

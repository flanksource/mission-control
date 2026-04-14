import { readFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';
import type { KitchenSinkData } from './kitchen-sink/KitchenSinkTypes.ts';

const __dirname = dirname(fileURLToPath(import.meta.url));
const raw = readFileSync(resolve(__dirname, 'kitchen-sink.json'), 'utf-8');
export const data = JSON.parse(raw) as KitchenSinkData;

export default data;

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  process.stdout.write(JSON.stringify(data));
}

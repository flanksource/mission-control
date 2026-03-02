import { readFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';
import yaml from 'js-yaml';
import type { Application } from './types.ts';

const __dirname = dirname(fileURLToPath(import.meta.url));
const raw = readFileSync(resolve(__dirname, 'fixtures/mission-control.yaml'), 'utf-8');
const data = yaml.load(raw) as Application;

console.log(JSON.stringify(data));

export default data;

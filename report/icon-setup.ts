import { addCollection } from '@iconify/react';
import { setFallbackIconProvider } from '@flanksource/clicky-ui';
import { Icon as FlanksourceIcon } from '@flanksource/icons/icon';
import carbon from '@iconify-json/carbon/icons.json';
import fluent from '@iconify-json/fluent/icons.json';
import iconoir from '@iconify-json/iconoir/icons.json';
import lucide from '@iconify-json/lucide/icons.json';
import mdi from '@iconify-json/mdi/icons.json';
import ph from '@iconify-json/ph/icons.json';
import ri from '@iconify-json/ri/icons.json';
import tabler from '@iconify-json/tabler/icons.json';
import vscodeIcons from '@iconify-json/vscode-icons/icons.json';

for (const collection of [carbon, fluent, iconoir, lucide, mdi, ph, ri, tabler, vscodeIcons]) {
  addCollection(collection as Parameters<typeof addCollection>[0]);
}

setFallbackIconProvider(FlanksourceIcon as Parameters<typeof setFallbackIconProvider>[0]);

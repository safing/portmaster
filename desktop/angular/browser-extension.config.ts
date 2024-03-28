import type { Configuration } from 'webpack';

module.exports = {
  entry: { background: { import: 'projects/portmaster-chrome-extension/src/background.ts', runtime: false } },
} as Configuration;

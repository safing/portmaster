import type { Configuration } from 'webpack';
const ExtensionReloader = require('webpack-ext-reloader');
const config = require('./browser-extension.config');

module.exports = {
  ...config,
  mode: 'development',
  plugins: [
    new ExtensionReloader({
      reloadPage: true, // Force the reload of the page also
      entries: { // The entries used for the content/background scripts or extension pages
        background: 'background',
      }
    })
  ]
} as Configuration;

import type { StorybookConfig } from '@storybook/nextjs-vite';

const config: StorybookConfig = {
  "stories": [
    "../{app,components}/**/*.mdx",
    "../{app,components}/**/*.stories.@(js|jsx|mjs|ts|tsx)"
  ],
  "addons": [
    "@storybook/addon-a11y",
    "@storybook/addon-docs",
    "msw-storybook-addon"
  ],
  "staticDirs": ["../public"],
  "framework": "@storybook/nextjs-vite"
};
export default config;
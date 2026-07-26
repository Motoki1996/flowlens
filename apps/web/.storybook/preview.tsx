import type { Preview } from '@storybook/nextjs-vite'
import { mswLoader } from 'msw-storybook-addon/csf3'
import '../app/globals.css'

const preview: Preview = {
  parameters: {
    controls: {
      matchers: {
       color: /(background|color)$/i,
       date: /Date$/i,
      },
    },
    // apps/web is App Router only; components using next/navigation (e.g.
    // LogoutButton's useRouter) need Storybook's app-dir router mock enabled,
    // otherwise the real next/navigation hook throws for lack of a mounted
    // AppRouterContext.
    nextjs: {
      appDirectory: true,
    },
  },
  loaders: [mswLoader()],
};

export default preview;
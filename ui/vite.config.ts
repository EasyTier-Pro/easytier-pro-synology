/// <reference types="vitest/config" />
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// The interface is built once per platform: DSM serves it from
// /3rdparty/easytier-pro/ via nginx, so every asset has to be referenced
// relatively (an absolute /assets/... would miss the package prefix); fnOS
// serves it under its own /app/easytier-pro/ prefix instead. Hash routing
// keeps the paths stable inside either prefix.
const platform = process.env.VITE_PLATFORM === 'fnos' ? 'fnos' : 'dsm'

export default defineConfig({
	base: platform === 'fnos' ? '/app/easytier-pro/' : './',
	plugins: [ vue() ],
	resolve: {
		alias: {
			'@': fileURLToPath(new URL('./src', import.meta.url)),
		},
	},
	build: {
		outDir: `dist-${platform}`,
		emptyOutDir: true,
		// The package serves these files straight from the build output, so keep
		// it readable and skip the source maps that would only add weight.
		sourcemap: false,
		chunkSizeWarningLimit: 1500,
	},
	test: {
		environment: 'happy-dom',
		include: [ 'src/**/*.test.ts' ],
		globals: false,
		setupFiles: [ 'src/test/setup.ts' ],
	},
})

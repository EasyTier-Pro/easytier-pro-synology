/// <reference types="vitest/config" />
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// The interface is served from /3rdparty/easytier-pro/ by DSM nginx, so every
// asset has to be referenced relatively: an absolute /assets/... would miss the
// package prefix. Hash routing keeps the paths stable inside that prefix.
export default defineConfig({
	base: './',
	plugins: [ vue() ],
	resolve: {
		alias: {
			'@': fileURLToPath(new URL('./src', import.meta.url)),
		},
	},
	build: {
		outDir: 'dist',
		emptyOutDir: true,
		// DSM serves these files straight from the package, so keep the output
		// readable and skip the source maps that would only add weight.
		sourcemap: false,
		chunkSizeWarningLimit: 1500,
	},
	test: {
		environment: 'happy-dom',
		include: [ 'src/**/*.test.ts' ],
		globals: false,
	},
})

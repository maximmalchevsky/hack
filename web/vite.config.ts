import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [sveltekit()],
	server: {
		port: 5173,
		host: '0.0.0.0',
		proxy: {
			'/api': {
				target: process.env.PUBLIC_API_URL || 'http://localhost:8080',
				changeOrigin: true
			},
			'/ws': {
				target: process.env.PUBLIC_WS_URL || 'ws://localhost:8080',
				ws: true,
				changeOrigin: true
			}
		}
	}
});

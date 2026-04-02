import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [sveltekit()],
	server: {
		proxy: {
			'/v1': {
				target: 'http://localhost:8080',
				changeOrigin: true
			},
			'/faces': {
				target: 'http://localhost:8080',
				changeOrigin: true
			},
			'/alexa': {
				target: 'http://localhost:8080',
				changeOrigin: true
			}
		}
	}
});

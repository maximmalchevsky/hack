/** @type {import('tailwindcss').Config} */
export default {
	content: ['./src/**/*.{html,js,svelte,ts}'],

	// Темы переключаются через атрибут data-theme на <html>.
	// Все цвета — CSS-переменные, реальные значения в src/lib/styles/tokens.css.
	theme: {
		extend: {
			fontFamily: {
				sans: [
					'-apple-system',
					'BlinkMacSystemFont',
					'Inter',
					'Segoe UI',
					'Roboto',
					'sans-serif'
				],
				mono: ['JetBrains Mono', 'SF Mono', 'Menlo', 'monospace']
			},
			colors: {
				bg: 'var(--bg)',
				surface: {
					DEFAULT: 'var(--surface)',
					2: 'var(--surface-2)',
					3: 'var(--surface-3)'
				},
				border: {
					DEFAULT: 'var(--border)',
					2: 'var(--border-2)',
					3: 'var(--border-3)'
				},
				text: {
					DEFAULT: 'var(--text)',
					2: 'var(--text-2)',
					3: 'var(--text-3)'
				},
				info: {
					bg: 'var(--info-bg)',
					border: 'var(--info-border)',
					text: 'var(--info-text)',
					strong: 'var(--info-strong)'
				},
				success: {
					bg: 'var(--success-bg)',
					border: 'var(--success-border)',
					text: 'var(--success-text)',
					strong: 'var(--success-strong)'
				},
				warning: {
					bg: 'var(--warning-bg)',
					border: 'var(--warning-border)',
					text: 'var(--warning-text)',
					strong: 'var(--warning-strong)'
				},
				danger: {
					bg: 'var(--danger-bg)',
					border: 'var(--danger-border)',
					text: 'var(--danger-text)',
					strong: 'var(--danger-strong)'
				},
				purple: {
					bg: 'var(--purple-bg)',
					text: 'var(--purple-text)'
				},
				teal: {
					bg: 'var(--teal-bg)',
					text: 'var(--teal-text)'
				}
			},
			borderRadius: {
				sm: 'var(--radius-sm)',
				md: 'var(--radius-md)',
				lg: 'var(--radius-lg)',
				xl: 'var(--radius-xl)'
			},
			boxShadow: {
				sm: 'var(--shadow-sm)',
				md: 'var(--shadow-md)',
				lg: 'var(--shadow-lg)'
			},
			spacing: {
				'sidebar-w': 'var(--sidebar-w)',
				'header-h': 'var(--header-h)'
			}
		}
	},
	plugins: []
};

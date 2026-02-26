import defaultTheme from 'tailwindcss/defaultTheme'

/** @type {import('tailwindcss').Config} */
export default {
  darkMode: ['class'],
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  theme: {
    container: {
      center: true,
      padding: '1rem',
      screens: {
        '2xl': '1400px',
      },
    },
    extend: {
      colors: {
        border: 'hsl(240 6% 90%)',
        input: 'hsl(240 6% 90%)',
        ring: 'hsl(240 5% 65%)',
        background: 'hsl(0 0% 100%)',
        foreground: 'hsl(240 10% 10%)',
        muted: {
          DEFAULT: 'hsl(240 6% 97%)',
          foreground: 'hsl(240 4% 40%)',
        },
        primary: {
          DEFAULT: 'hsl(240 10% 10%)',
          foreground: 'hsl(0 0% 98%)',
        },
        secondary: {
          DEFAULT: 'hsl(240 6% 96%)',
          foreground: 'hsl(240 10% 10%)',
        },
        accent: {
          DEFAULT: 'hsl(240 6% 96%)',
          foreground: 'hsl(240 10% 10%)',
        },
        destructive: {
          DEFAULT: 'hsl(0 84% 60%)',
          foreground: 'hsl(0 0% 98%)',
        },
      },
      borderRadius: {
        lg: '0.75rem',
        md: '0.55rem',
        sm: '0.4rem',
      },
      fontFamily: {
        sans: ['Inter', ...defaultTheme.fontFamily.sans],
      },
      boxShadow: {
        card: '0 1px 2px hsl(240 10% 10% / 0.06)',
      },
    },
  },
  plugins: [],
}

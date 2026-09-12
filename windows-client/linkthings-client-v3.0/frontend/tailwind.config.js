/** Nocturne → Tailwind theme (LinkThings client).
 *  Every value here is the design system's token, so Tailwind classes and the
 *  mockup stay in step. Prefer `bg-surface`, `text-muted`, `rounded-md`,
 *  `shadow-edge` over arbitrary values. */
export default {
  content: ["./index.html", "./src/**/*.{vue,js,ts}"],
  theme: {
    extend: {
      colors: {
        bg: "#161826",
        surface: "#1c1e2c",
        "surface-raised": "#232532",
        sunken: "#12141f",
        ink: "#e9e9ed",
        muted: "#9397ab",
        faint: "#75798c",
        ghost: "#595d6c",
        line: "rgba(233,233,237,0.08)",
        danger: "#d98484",
        accent: {
          DEFAULT: "#9184d9",
          100: "#f5f4ff", 200: "#e7e5fe", 300: "#d2cefd", 400: "#b5abfc",
          500: "#968ae0", 600: "#796cbf", 700: "#5d5294", 800: "#423a6a", 900: "#2b2741"
        },
        neutral: {
          100: "#f3f5fe", 200: "#e4e7f5", 300: "#cfd3e5", 400: "#b2b6ca",
          500: "#9397ab", 600: "#75798c", 700: "#595d6c", 800: "#3f424d", 900: "#292b31"
        }
      },
      fontFamily: {
        sans: ["Inter", "system-ui", "sans-serif"],
        mono: ["ui-monospace", "Menlo", "monospace"]
      },
      /* Nocturne density 0.70× */
      spacing: { 1: "2.8px", 2: "5.6px", 3: "8.4px", 4: "11.2px", 6: "16.8px", 8: "22.4px" },
      borderRadius: { sm: "4px", md: "8px", lg: "14px" },
      boxShadow: {
        edge: "0 0 0 1px #3f424d",
        "edge-strong": "0 0 0 1px #595d6c",
        md: "0 0 0 1px #595d6c, 0 6px 18px rgba(0,0,0,0.55)",
        lg: "0 0 0 1px #9397ab, 0 16px 40px rgba(0,0,0,0.65)",
        glow: "0 0 22px rgba(145,132,217,0.32)"
      },
      keyframes: {
        flowDown: { to: { backgroundPositionY: "16px" } },
        pulseSoft: { "0%,100%": { opacity: ".4" }, "50%": { opacity: "1" } }
      },
      animation: {
        "flow-down": "flowDown .8s linear infinite",
        "pulse-soft": "pulseSoft 1.1s ease-in-out infinite"
      }
    }
  }
};

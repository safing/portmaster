const plugin = require("tailwindcss/plugin");

module.exports = {
  content: [
    "./src/**/*.{html,scss,css,ts}",
    "./projects/**/*.{html,scss,css,ts}",
  ],
  theme: {
    colors: {
      transparent: "transparent",
      current: "currentColor",
      white: "#ffffff",
      background: "#121213",

      gray: {
        100: "#131111",
        200: "#1b1b1b",
        300: "#222222",
        400: "#2c2c2c",
        500: "#474747",
        600: "#888888",
        700: "#ababab",
        DEFAULT: "#ababab",
      },

      green: {
        100: "#143d24",
        200: "#18823d",
        300: "#1de966",
        DEFAULT: "#18823d",
      },

      red: {
        100: "#3d1414",
        200: "#811818",
        300: "#e01d1d",
        DEFAULT: "#d12e2e",
      },

      yellow: {
        100: "#3d3a14",
        200: "#827918",
        300: "#e9d81d",
        DEFAULT: "#e9d81d",
      },

      cyan: {
        100: "#b2ebf2",
        200: "#80deea",
        300: "#4dd0e1",
        400: "#26c6da",
        500: "#00bcd4",
        600: "#00acc1",
        700: "#0097a7",
        800: "#00838f",
        900: "#006064",
      },

      deepPurple: {
        50: "#ede7f6",
        100: "#d1c4e9",
        200: "#b39ddb",
        300: "#9575cd",
        400: "#7e57c2",
        500: "#673ab7",
        600: "#5e35b1",
        700: "#512da8",
        800: "#4527a0",
        900: "#311b92",
      },

      blue: {
        DEFAULT: "#4e97fa",
      },

      // Legacy color definitions

      // The overall application background color

      // Text shades
      cards: {
        primary: "var(--cards-primary)",
        secondary: "var(--cards-secondary)",
        tertiary: "var(--cards-tertiary)",
      },

      buttons: {
        icon: "var(--button-icon)",
        dark: "var(--button-dark)",
        light: "var(--button-light)",
      },

      info: {
        green: "var(--info-green)",
        red: "var(--info-red)",
        gray: "var(--info-gray)",
        blue: "var(--info-blue)",
        yellow: "var(--info-yellow)",
      },
    },
    textColor: (theme) => {
      return {
        primary: theme("colors.white"),
        secondary: theme("colors.gray.700"),
        tertiary: theme("colors.gray.600"),

        ...theme("colors"),
      };
    },
    extend: {
      boxShadow: {
        xs: "0 0 0 1px rgba(0, 0, 0, 0.05)",
        "inner-xs": "inset 0 2px 4px 0 rgba(0, 0, 0, 0.16)",
      },
      fontSize: {
        xxs: "0.7rem",
      },
    },
  },
  plugins: [
    plugin(function ({ addVariant, theme }) {
      Object.keys(theme("screens")).forEach((key) => {
        addVariant("sfng-" + key, ".min-width-" + theme("screens")[key] + " &");
      });
    }),
  ],
};

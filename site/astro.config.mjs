import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import { fileURLToPath } from "node:url";

export default defineConfig({
  vite: {
    resolve: {
      alias: [
        { find: "@site", replacement: fileURLToPath(new URL("./src", import.meta.url)) },
        {
          find: /^@astrojs\/starlight\/components$/,
          replacement: fileURLToPath(new URL("./node_modules/@astrojs/starlight/components.ts", import.meta.url)),
        },
      ],
    },
  },
  site: "https://docs.laminara.dev",
  trailingSlash: "always",
  integrations: [
    starlight({
      title: "Laminara",
      description: "Лаунчер Minecraft и сервер, который готовит для него сборки",
      favicon: "/favicon.svg",
      logo: {
        light: "../docs/logo/light.svg",
        dark: "../docs/logo/dark.svg",
        replacesTitle: true,
      },
      social: [{ icon: "github", label: "GitHub", href: "https://github.com/MrLeonardosMi/Laminara" }],
      editLink: { baseUrl: "https://github.com/MrLeonardosMi/Laminara/edit/main/docs/" },
      lastUpdated: true,
      pagination: true,
      defaultLocale: "root",
      locales: { root: { label: "Русский", lang: "ru" } },
      customCss: ["./src/styles/docs.css"],
      sidebar: [
        { label: "Знакомство", items: ["index", "quickstart", "console", "architecture"] },
        { label: "Сервер", items: ["server/install", "server/configuration", "server/production", "server/signing", "server/machines"] },
        { label: "Сборки", items: ["builds/preparing", "builds/loaders", "builds/publishing", "builds/settings", "builds/features", "builds/access"] },
        { label: "Лаунчер", items: ["launcher", "launcher/building", "launcher/updates", "launcher/news"] },
        { label: "Вход игроков", items: ["auth/adapters", "auth/yggdrasil"] },
        { label: "Хранилище", items: ["storage"] },
        { label: "Модули", items: ["modules"] },
        { label: "Справочник", items: ["reference/cli", "reference/config"] },
      ],
    }),
  ],
});

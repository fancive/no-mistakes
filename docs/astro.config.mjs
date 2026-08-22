import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";

export default defineConfig({
  site: "https://kunchenguid.github.io",
  base: "/no-mistakes",
  integrations: [
    starlight({
      title: "no-mistakes lean guard",
      customCss: ["./src/styles/custom.css"],
      social: {
        github: "https://github.com/kunchenguid/no-mistakes",
        discord: "https://discord.gg/Wsy2NpnZDu",
        "x.com": "https://x.com/kunchenguid",
      },
      sidebar: [
        {
          label: "Start Here",
          items: [
            { label: "Introduction", slug: "start-here/introduction" },
            { label: "Quick Start", slug: "start-here/quick-start" },
            { label: "Installation", slug: "start-here/installation" },
          ],
        },
        {
          label: "Guides",
          items: [
            { label: "Configuration", slug: "guides/configuration" },
            { label: "Provider Integration", slug: "guides/provider-integration" },
            { label: "Legacy Cleanup", slug: "guides/legacy-cleanup" },
            { label: "Troubleshooting", slug: "guides/troubleshooting" },
          ],
        },
        {
          label: "Reference",
          items: [
            { label: "CLI Commands", slug: "reference/cli" },
            { label: "Repo Config", slug: "reference/repo-config" },
            { label: "Environment Variables", slug: "reference/environment" },
          ],
        },
      ],
    }),
  ],
});

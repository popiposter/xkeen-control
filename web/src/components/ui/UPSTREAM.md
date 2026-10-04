# shadcn source assets

`shadcn-tailwind.css` is copied without changes from the MIT-licensed
`shadcn@4.21.1/dist/tailwind.css` npm package. Its SHA256 is
`4c371f7a1ff5d219ae2f7ff28bd256b4346fd546fe46fbae22092e57db2f0fae`
(16,340 bytes). The license is retained in [LICENSE.md](LICENSE.md).

The CLI is a component-generation tool, not a build dependency. Run it explicitly
when adding components, then review the generated source and dependencies.
Keeping the stylesheet locally preserves the standard shadcn utilities without
installing the CLI and its registry/glob dependencies in every build.

Tooltip, Tabs and DropdownMenu were generated from the official `base-nova`
registry on 2026-10-04. Their utility import is normalized to the project's
`@/lib/utils` alias. They use the existing pinned Base UI and lucide dependencies.
Application portals use the existing CSPProvider boundary.

Plan: Migrate Frontend from PrimeVue to shadcn-vue
TL;DR: Incrementally replace PrimeVue v3 with shadcn-vue across 42 Vue components (23 components + 19 views), migrating to TypeScript + Composition API (<script setup>) along the way. The theme picker (30+ themes, 674-line ThemeConfig.vue) is replaced by a dark/light toggle defaulting to prefers-color-scheme. PrimeFlex utilities are swapped for Tailwind CSS. moment.js → date-fns, primeicons → Lucide. Each step keeps the app functional and deployable.

Steps

Phase 1 — Foundation Setup
Add TypeScript support: Add typescript, vue-tsc, and @vue/tsconfig to dev dependencies. Create tsconfig.json and tsconfig.app.json. Update vite.config.js → vite.config.ts. Rename src/main.js → main.ts.

Install Tailwind CSS v4: Add tailwindcss, @tailwindcss/vite. Configure in Vite config. Create a src/assets/index.css with @import "tailwindcss" and CSS variable definitions for shadcn-vue theming (light + dark color tokens using the oklch palette). Import in main.ts. PrimeFlex and Tailwind can coexist temporarily during migration.

Install shadcn-vue tooling: Add reka-ui (Radix Vue successor), class-variance-authority, clsx, tailwind-merge. Create src/lib/utils.ts with the standard cn() helper. Set up components.json for shadcn-vue CLI. Run npx shadcn-vue@latest init to scaffold the base config.

Install icon & date libraries: Add lucide-vue-next, date-fns. Keep moment and primeicons temporarily — they'll be removed per-component.

Update ESLint: Update eslint.config.js for TypeScript support (@typescript-eslint/parser, @vue/eslint-config-typescript).

Phase 2 — Dark/Light Mode Toggle (replaces theme system)
Create composable src/composables/useColorMode.ts: Reactive mode ref ('light' | 'dark' | 'system'), persisted to localStorage. Applies/removes class dark on <html>. Watches prefers-color-scheme media query when mode is 'system'. Defaults to 'system' on first visit.

Delete theme infrastructure: Remove ThemeConfig.vue (674 lines), layout/composables/layout.js (theme + scale state), the <link id="theme-css"> tag from index.html, and the entire public/themes/ directory (~30 theme CSS files). Remove the theme gear icon and sidebar toggle from AppHeader.vue.

Add shadcn-vue Button and DropdownMenu components (via CLI: npx shadcn-vue@latest add button dropdown-menu). Build a ModeToggle.vue component using DropdownMenu with three options (Light / Dark / System) using Lucide Sun, Moon, Monitor icons. Place it in AppHeader.vue where the theme gear icon was.

Define CSS variables: In the new Tailwind CSS file, define --background, --foreground, --card, --primary, --destructive, etc. under :root (light) and .dark (dark) selectors per shadcn-vue's theming convention.

Phase 3 — Layout & Navigation
Rewrite AppHeader.vue: Replace PrimeVue Menu (popup nav dropdowns) with shadcn-vue NavigationMenu or DropdownMenu. Replace primeicons with Lucide icons. Migrate to <script setup lang="ts">. Replace PrimeFlex layout classes (flex, align-items-center, etc.) with Tailwind equivalents (flex, items-center, etc.).

Rewrite AppFooter.vue: Simple component — migrate to <script setup lang="ts">, swap PrimeFlex for Tailwind classes.

Rewrite App.vue: Remove PrimeVue global component refs (PVToast, ConfirmDialog), add shadcn-vue Toaster (from sonner). Migrate to <script setup lang="ts">.

Replace global styles: Rewrite main.css — remove all PrimeVue overrides (.p-card, .p-inputtext, .p-toast, etc.), define layout constraints with Tailwind's @apply or utility classes in App.vue.

Phase 4 — Toast & Confirmation Systems
Install sonner for toasts: Add vue-sonner package. Add shadcn-vue Sonner component (CLI: npx shadcn-vue@latest add sonner). Create a src/composables/useAppToast.ts composable wrapping toast() from vue-sonner with success(), error(), info() helpers to ease migration from this.$toast.add().

Replace ConfirmDialog: Add shadcn-vue AlertDialog component (CLI: npx shadcn-vue@latest add alert-dialog). Create a src/composables/useConfirmDialog.ts composable that provides a promise-based confirm({ title, description }) API wrapping AlertDialog, replacing PrimeVue's this.$confirm.require() pattern. The composable will be used in all 6 files that currently use $confirm.

Phase 5 — Form Components
Add shadcn-vue form components via CLI: input, label, checkbox, select, textarea, toggle-group, badge, card, separator.

Rewrite form-heavy views (migrate to TS + <script setup>, replace PrimeVue components, swap PrimeFlex → Tailwind, replace float labels with standard <Label> above <Input>):

LoginPage.vue — InputText → shadcn Input, Button → shadcn Button, Card → shadcn Card
UserRegistrationCard.vue (used by RegisterPage) — same input/button/card swap, Vuelidate stays
NewRepeaterPage.vue — add SelectButton → shadcn ToggleGroup
NewTalkgroupsPage.vue — add MultiSelect → custom combobox built from shadcn Popover + Command + Badge
NewOpenBridgePeerPage.vue — Dropdown → shadcn Select, Checkbox → shadcn Checkbox
Rewrite all setup wizard components (11 files, all form-heavy):

GeneralSettings.vue, HTTPSettings.vue, DMRSettings.vue, SMTPSettings.vue, DatabaseSettings.vue, RedisSettings.vue, MetricsSettings.vue, PProfSettings.vue
MMDVMSettings.vue, OpenBridgeSettings.vue, IPSCSettings.vue
CORSSettings.vue, RobotsTXTSettings.vue
Keep the v-model / modelValue pattern — works identically with shadcn-vue inputs
Dropdown → shadcn Select, Checkbox → shadcn Checkbox, TextArea → shadcn Textarea
Rewrite SetupWizard.vue — the stepper/card wrapper for setup components. Replace Card + Button with shadcn equivalents. Replace local Toast import with sonner.

Phase 6 — Data Tables (largest effort)
Install @tanstack/vue-table. Create a reusable DataTable.vue wrapper component in src/components/ui/data-table/ that combines shadcn Table + TanStack for column definitions, pagination, sorting, and filtering. Include a DataTablePagination.vue sub-component.

Rewrite table components one at a time (each has lazy-loading pagination, search, and action buttons):

LastHeardTable.vue — simplest table (read-only, time formatting with date-fns replacing moment)
RepeaterTable.vue — has Chip (→ shadcn Badge), MultiSelect (→ custom combobox), Dropdown (→ Select), expandable rows
TalkgroupTable.vue — has MultiSelect, inline editing, search
UserTable.vue — has Checkbox inline edits, approve/delete confirm actions
PeerTable.vue — similar pattern with checkbox edits and delete confirm
Rewrite thin view wrappers: All views that just wrap a table in a Card — RepeatersPage (user), RepeatersPage (admin), TalkgroupsPage, OwnedTalkgroupsPage, TalkgroupsPage (admin), UsersPage, UsersApprovalPage, OpenBridgePeersPage (both), RepeaterDetailsPage.

Phase 7 — Remaining Views & Components
Rewrite MainPage.vue and LastHeard.vue — Card wrappers with WebSocket integration. Keep WebSocket logic unchanged.

Rewrite InitialUserPage.vue — setup user creation form.

Phase 8 — Infrastructure Cleanup
Migrate stores to TypeScript: Rename store/index.mjs → store/index.ts. Type useUserStore state. Remove useSettingsStore if still unused, or repurpose it for color mode persistence.

Migrate services to TypeScript: Rename API.js → API.ts, util.js → util.ts, ws.js → ws.ts. Add type annotations. Move axios from devDependencies to dependencies.

Migrate router to TypeScript: Rename router/index.js → index.ts, routes.mjs → routes.ts.

Remove PrimeVue entirely: Uninstall primevue, primeflex, primeicons, moment. Remove all PrimeVue plugin registration from main.ts. Remove ToastService, DialogService, ConfirmationService registrations. Delete the <link id="theme-css"> fallback if still present in index.html. Remove primevue/resources/primevue.min.css import.

Clean up CSS: Remove old main.css PrimeVue overrides. Consolidate all global styles into the Tailwind CSS entry point.

Update SPDX headers: Ensure all new .ts/.vue files carry the required license header per project conventions.

Phase 9 — Testing & Verification
Update E2E test: Update example.cy.js smoke test if selectors changed. Update home.cy.js screenshot test.

Add dark mode E2E test: Verify the toggle cycles through system → light → dark and persists across page reload.

Verification

npm run build succeeds with no TypeScript errors after each phase
npm run lint passes after each phase
npm run dev — manual visual check that every route renders correctly
npm run test:unit and npx cypress run pass
Dark/light toggle: defaults to system, persists choice to localStorage, applies dark class on <html>
All forms still validate and submit correctly (Vuelidate unchanged)
All data tables still paginate, sort, filter, and perform inline edits
WebSocket real-time updates still work on LastHeard and MainPage
Go build (make build) still embeds the frontend correctly
SPDX license headers present on all new/modified files
Decisions

Composition API (<script setup lang="ts">) over Options API — natural pairing with shadcn-vue composable patterns
TypeScript — better DX with shadcn-vue's typed component props
TanStack Table over plain shadcn Table — needed for lazy pagination, sorting, filtering, expandable rows
Standard labels above inputs over float labels — simpler, accessible, no custom CSS needed
date-fns over dayjs/moment — tree-shakeable, modern
Lucide icons over Heroicons — standard shadcn-vue pairing
Incremental migration — each phase is independently deployable; PrimeFlex and Tailwind coexist temporarily
vue-sonner replaces PrimeVue ToastService; AlertDialog replaces ConfirmDialog service
reka-ui as the headless primitive layer (successor to Radix Vue, which shadcn-vue now uses)
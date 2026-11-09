<script lang="ts">
  import { app } from "../lib/app.svelte";
  import { t } from "../lib/i18n";
  import InviteForm from "../components/InviteForm.svelte";
</script>

<section class="page">
  <h1 class="h3">{t(app.lang, "invite.title")}</h1>
  <InviteForm ondone={() => (app.view = "home")} />
</section>

<style>
  .page { display: flex; flex-direction: column; gap: var(--dot-space-4); padding: var(--dot-space-5) var(--dot-space-4) var(--dot-space-4); }
</style>

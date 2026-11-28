<script lang="ts">
  import type { AppStore } from '../lib/store.svelte';
  import { errorMessage, i18n, parseLocale, t, toErrorRef, type Locale, type MsgId } from '../i18n';
  let { store }: { store: AppStore } = $props();

  const options: { id: Locale; label: MsgId }[] = [
    { id: 'en', label: 'settings.languageEnglish' },
    { id: 'vi', label: 'settings.languageVietnamese' },
  ];

  $effect(() => {
    const l = parseLocale(store.settings?.language);
    if (l) i18n.set(l);
  });

  async function choose(e: Event): Promise<void> {
    const select = e.currentTarget as HTMLSelectElement;
    const l = parseLocale(select.value);
    const prev = i18n.lang;
    if (!l || l === prev) return;
    i18n.set(l);
    try {
      await store.api.call('settings.update', { patch: { language: l } });
    } catch (err) {
      i18n.set(prev);
      select.value = prev;
      store.error = errorMessage(toErrorRef(err));
    }
  }
</script>

<div class="setting-row">
  <div><strong>{t('settings.language')}</strong><p>{t('settings.languageDesc')}</p></div>
  <select class="input" aria-label={t('settings.language')} value={i18n.lang} onchange={choose}>
    {#each options as o (o.id)}
      <option value={o.id} lang={o.id}>{t(o.label)}</option>
    {/each}
  </select>
</div>

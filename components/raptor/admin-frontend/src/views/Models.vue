<script setup lang="ts">
import { onMounted, onUnmounted, ref } from "vue";
import { api } from "../api";
const provider = ref<any>({
  status: "disconnected",
  models: [],
  enabled: [],
  policyVersion: 0,
});
const enabled = ref<string[]>([]),
  defaultModelId = ref(""),
  challenge = ref<any>(null),
  redirectUrl = ref("");
const loading = ref(true),
  busy = ref(false),
  error = ref(""),
  notice = ref("");
let timer: ReturnType<typeof setTimeout> | undefined,
  active = true,
  generation = 0;
function stopPolling() {
  generation++;
  if (timer) clearTimeout(timer);
  timer = undefined;
}
async function load() {
  const result = await api("admin/model-providers");
  if (!active) return;
  provider.value = result.providers.find(
    (p: any) => p.providerId === "codex",
  ) || { status: "disconnected", models: [], enabled: [], policyVersion: 0 };
  enabled.value = [...(provider.value.enabled || [])];
  defaultModelId.value = provider.value.defaultModelId || "";
}
onMounted(async () => {
  try {
    await load();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
});
onUnmounted(() => {
  active = false;
  stopPolling();
  redirectUrl.value = "";
});
async function action(path: string, method = "POST", body: unknown = {}) {
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    await api("admin/model-providers/codex/" + path, method, body);
    await load();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
async function connect(browser: boolean) {
  busy.value = true;
  error.value = "";
  stopPolling();
  try {
    const next = await api(
      "admin/model-providers/codex/" +
        (browser ? "browser-connect" : "connect"),
      "POST",
      {},
    );
    if (!active) return;
    challenge.value = { ...next, browser };
    poll(generation, next.sessionId);
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
function poll(epoch: number, sessionId: string) {
  timer = setTimeout(async () => {
    if (!active || epoch !== generation) return;
    try {
      const state = await api(
        "admin/model-providers/codex/connect/" + encodeURIComponent(sessionId),
      );
      if (!active || epoch !== generation) return;
      if (state.status === "pending") {
        poll(epoch, sessionId);
        return;
      }
      stopPolling();
      challenge.value = null;
      redirectUrl.value = "";
      if (state.status === "connected") await load();
      else notice.value = "Sign-in " + state.status;
    } catch (e) {
      if (!active || epoch !== generation) return;
      stopPolling();
      error.value = (e as Error).message;
    }
  }, 2000);
}
async function cancel() {
  const id = challenge.value?.sessionId;
  if (!id) return;
  stopPolling();
  busy.value = true;
  error.value = "";
  try {
    await api(
      "admin/model-providers/codex/connect/" + encodeURIComponent(id),
      "DELETE",
    );
    challenge.value = null;
    redirectUrl.value = "";
    notice.value = "Sign-in cancelled";
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
async function complete() {
  busy.value = true;
  error.value = "";
  const submitted = redirectUrl.value.trim();
  redirectUrl.value = "";
  try {
    await api(
      "admin/model-providers/codex/connect/" +
        encodeURIComponent(challenge.value.sessionId) +
        "/complete",
      "POST",
      { redirectUrl: submitted },
    );
    notice.value = "Completing sign-in…";
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
async function saveModels() {
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    await api("admin/model-providers/codex/models", "PUT", {
      expectedVersion: provider.value.policyVersion,
      enabled: [...enabled.value],
      defaultModelId: defaultModelId.value,
    });
    await load();
    notice.value = "Model settings saved.";
  } catch (e) {
    const message = (e as Error).message;
    try {
      await load();
    } catch {}
    error.value = message;
  } finally {
    busy.value = false;
  }
}
function safeLink(value: unknown) {
  try {
    const url = new URL(String(value));
    return url.protocol === "https:" && !url.username && !url.password
      ? url.href
      : undefined;
  } catch {
    return undefined;
  }
}
</script>
<template>
  <a-alert v-if="error" :message="error" type="error" show-icon /><a-alert
    v-if="notice"
    :message="notice"
    type="info"
    show-icon
  /><a-card title="AI model providers" :loading="loading"
    ><h2>OpenAI Codex</h2>
    <p role="status">
      <a-tag :color="provider.status === 'connected' ? 'green' : 'default'">{{
        provider.status
      }}</a-tag
      >{{ provider.accountLabel || "" }}
    </p>
    <div class="actions">
      <a-button
        type="primary"
        :disabled="busy || !!challenge"
        @click="connect(true)"
        >Connect in browser</a-button
      ><a-button :disabled="busy || !!challenge" @click="connect(false)"
        >Connect with device code</a-button
      ><a-button
        :disabled="busy || !!challenge || provider.status !== 'connected'"
        @click="action('disconnect')"
        >Disconnect</a-button
      ><a-button
        :disabled="busy || !!challenge || provider.status !== 'connected'"
        @click="action('discover')"
        >Refresh available models</a-button
      >
    </div>
    <a-card
      v-if="challenge"
      :title="challenge.browser ? 'Browser sign-in' : 'Device sign-in'"
      class="section-space"
      ><a
        v-if="safeLink(challenge.authorizationUrl || challenge.verificationUrl)"
        :href="
          safeLink(challenge.authorizationUrl || challenge.verificationUrl)
        "
        target="_blank"
        rel="noopener noreferrer"
        >{{
          challenge.browser ? "Open OpenAI sign-in" : "Open verification page"
        }}</a
      >
      <p v-if="challenge.userCode">
        Code: <strong>{{ challenge.userCode }}</strong>
      </p>
      <p>Expires: {{ new Date(challenge.expiresAt).toLocaleString() }}</p>
      <form v-if="challenge.browser" @submit.prevent="complete">
        <p class="muted">
          After approving sign-in, the browser may show a localhost error. Copy
          its full address-bar URL and paste it below. Keep that URL private.
        </p>
        <label
          >Final localhost redirect URL<a-input
            v-model:value="redirectUrl"
            type="url"
            required
            autocomplete="off"
            :spellcheck="false"
        /></label>
        <div class="actions">
          <a-button type="primary" html-type="submit" :loading="busy"
            >Complete sign-in</a-button
          >
        </div>
      </form>
      <a-button :disabled="busy" @click="cancel"
        >Cancel sign-in</a-button
      ></a-card
    >
    <form
      v-if="provider.models?.length"
      @submit.prevent="saveModels"
      class="section-space"
    >
      <h3>Models</h3>
      <label
        v-for="model in provider.models"
        :key="model.modelId"
        class="model-option"
        ><input
          type="checkbox"
          v-model="enabled"
          :value="model.modelId"
          :disabled="!model.available || busy"
        />{{ model.displayName
        }}{{ model.available ? "" : " (unavailable)" }}</label
      ><label style="max-width: 500px"
        >Default model<a-select
          v-model:value="defaultModelId"
          :options="[
            { value: '', label: 'No default' },
            ...provider.models
              .filter((m: any) => m.available)
              .map((m: any) => ({ value: m.modelId, label: m.displayName })),
          ]"
      /></label>
      <div class="actions">
        <a-button type="primary" html-type="submit" :loading="busy"
          >Save enabled models</a-button
        >
      </div>
    </form></a-card
  >
</template>

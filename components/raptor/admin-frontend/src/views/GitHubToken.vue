<script setup lang="ts">
import { onMounted, ref } from "vue";
import { api } from "../api";
const token = ref(""),
  status = ref<any>(null),
  loading = ref(true),
  busy = ref(false),
  error = ref(""),
  success = ref("");
async function load() {
  status.value = await api("admin/github-pat");
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
async function change(remove = false) {
  busy.value = true;
  error.value = "";
  success.value = "";
  const submitted = token.value;
  token.value = "";
  try {
    await api(
      "admin/github-pat",
      remove ? "DELETE" : "PUT",
      remove ? undefined : { token: submitted },
    );
    await load();
    success.value = remove ? "Token deleted." : "Token saved.";
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <a-alert v-if="error" :message="error" type="error" show-icon /><a-alert
    v-if="success"
    :message="success"
    type="success"
    show-icon
  /><a-card title="GitHub personal access token" :loading="loading"
    ><p class="muted">
      Used by repository change attempts to fetch public repositories, push a
      branch, and open a pull request. The agent in its sandbox can read this
      token.
    </p>
    <p role="status">
      {{
        status?.configured
          ? "Configured for " +
            status.owner +
            "; verified " +
            new Date(status.verifiedAt).toLocaleString()
          : "No token configured"
      }}
    </p>
    <form @submit.prevent="change()">
      <label style="max-width: 620px"
        >Personal access token<a-input-password
          v-model:value="token"
          required
          autocomplete="off"
          :spellcheck="false"
      /></label>
      <div class="actions">
        <a-button type="primary" html-type="submit" :loading="busy"
          >Save or replace</a-button
        ><a-button
          danger
          :disabled="!status?.configured || busy"
          @click="change(true)"
          >Delete token</a-button
        >
      </div>
    </form></a-card
  >
</template>

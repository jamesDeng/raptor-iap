<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { useRoute } from "vue-router";
import { VbenAdminLayout } from "@vben-core/layout-ui";
import { currentUser, restoreSession, signIn, signOut } from "./api";
import { menu } from "./router";
const route = useRoute();
const mobile = ref(false);
const media = window.matchMedia("(max-width: 760px)");
function updateMobile() {
  mobile.value = media.matches;
}
updateMobile();
media.addEventListener("change", updateMobile);
onUnmounted(() => media.removeEventListener("change", updateMobile));
const loading = ref(true),
  busy = ref(false),
  error = ref("");
const username = ref(""),
  password = ref(""),
  collapsed = ref(false);
const title = computed(() => String(route.meta.title || "Administration"));
const selected = computed(() => [
  route.path.startsWith("/environments/") ? "/environments" : route.path,
]);
onMounted(async () => {
  try {
    await restoreSession();
  } catch (e) {
    if ((e as Error).message !== "Sign in required")
      error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
});
async function login() {
  busy.value = true;
  error.value = "";
  try {
    await signIn(username.value, password.value);
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    password.value = "";
    busy.value = false;
  }
}
async function logout() {
  busy.value = true;
  try {
    await signOut();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <a-config-provider
    :theme="{
      token: {
        colorPrimary: '#087a75',
        borderRadius: 6,
        fontFamily: 'Inter, Segoe UI, sans-serif',
      },
    }"
  >
    <div v-if="loading" class="session-loading" role="status">
      <a-spin size="large" />
      <p>Checking session…</p>
    </div>
    <main v-else-if="!currentUser" class="login-screen">
      <div class="login-brand">
        Raptor<span>.</span>
        <p>Infrastructure operations</p>
      </div>
      <a-card class="login-card" title="Administrator sign in">
        <p class="muted">
          Manage platform access and environment configuration.
        </p>
        <a-alert v-if="error" :message="error" type="error" show-icon />
        <form @submit.prevent="login" class="form-stack">
          <label
            >Username<a-input
              v-model:value="username"
              required
              autocomplete="username"
          /></label>
          <label
            >Password<a-input-password
              v-model:value="password"
              required
              autocomplete="current-password"
          /></label>
          <a-button type="primary" html-type="submit" :loading="busy"
            >Sign in</a-button
          >
        </form>
      </a-card>
    </main>
    <VbenAdminLayout
      v-else
      v-model:sidebar-collapse="collapsed"
      :key="mobile ? route.path : 'desktop'"
      :is-mobile="mobile"
      :sidebar-fixed-button="false"
      :sidebar-logo-visible="true"
      :sidebar-width="240"
      :header-height="64"
      :tabbar-enable="false"
      :sidebar-collapsed-button="false"
      :header-toggle-sidebar-button="mobile"
      sidebar-theme="dark"
    >
      <template #logo
        ><RouterLink to="/users" class="brand"
          >{{ collapsed ? "R." : "Raptor."
          }}<small v-if="!collapsed">ADMINISTRATION</small></RouterLink
        ></template
      >
      <template #menu
        ><a-menu
          theme="dark"
          mode="inline"
          :selected-keys="selected"
          :inline-collapsed="!mobile && collapsed"
          class="admin-menu"
          ><a-menu-item v-for="item in menu" :key="item.path"
            ><RouterLink :to="item.path">{{
              item.title
            }}</RouterLink></a-menu-item
          ></a-menu
        ></template
      >
      <template #header
        ><div class="admin-header">
          <a-button
            v-if="!mobile"
            aria-label="Toggle navigation"
            @click="collapsed = !collapsed"
            >☰</a-button
          ><span>Administration</span>
          <div class="account">
            <span>{{ currentUser.username }}</span
            ><a-button :loading="busy" @click="logout">Sign out</a-button>
          </div>
        </div></template
      >
      <template #content
        ><main class="page-content">
          <header class="page-title">
            <p class="eyebrow">RAPTOR / ADMINISTRATION</p>
            <h1>{{ title }}</h1>
          </header>
          <a-alert
            v-if="error"
            :message="error"
            type="error"
            show-icon
            closable
            @close="error = ''"
          /><RouterView :key="route.fullPath" /></main
      ></template>
    </VbenAdminLayout>
  </a-config-provider>
</template>

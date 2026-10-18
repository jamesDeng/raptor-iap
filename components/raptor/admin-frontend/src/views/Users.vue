<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { api } from "../api";
const users = ref<any[]>([]),
  loading = ref(true),
  busy = ref(false),
  error = ref(""),
  success = ref("");
const form = reactive({ username: "", password: "", role: "user" });
async function load() {
  users.value = await api("users");
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
async function save() {
  busy.value = true;
  error.value = "";
  success.value = "";
  try {
    await api("users", "POST", { ...form });
    form.username = "";
    form.role = "user";
    await load();
    success.value = "User created.";
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    form.password = "";
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
  />
  <a-card title="Create user"
    ><p class="muted">Create accounts and assign their access level.</p>
    <form @submit.prevent="save">
      <div class="form-grid">
        <label
          >Username<a-input
            v-model:value="form.username"
            required
            autocomplete="off" /></label
        ><label
          >Password<a-input-password
            v-model:value="form.password"
            required
            :minlength="12"
            autocomplete="new-password" /></label
        ><label
          >Role<a-select
            v-model:value="form.role"
            :options="[
              { value: 'user', label: 'User' },
              { value: 'admin', label: 'Admin' },
            ]"
        /></label>
      </div>
      <div class="actions">
        <a-button html-type="submit" type="primary" :loading="busy"
          >Create user</a-button
        >
      </div>
    </form></a-card
  >
  <a-card title="Existing users" class="section-space" :loading="loading"
    ><div v-if="users.length" class="table-scroll">
      <table>
        <thead>
          <tr>
            <th>Username</th>
            <th>Role</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="user in users" :key="user.username">
            <td>{{ user.username }}</td>
            <td>
              <a-tag :color="user.role === 'admin' ? 'cyan' : 'default'">{{
                user.role
              }}</a-tag>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <a-empty v-else description="No users yet."
  /></a-card>
</template>

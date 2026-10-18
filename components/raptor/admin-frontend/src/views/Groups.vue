<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { api } from "../api";
const groups = ref<any[]>([]),
  loading = ref(true),
  busy = ref(false),
  error = ref(""),
  success = ref("");
const form = reactive({ code: "", name: "" });
async function load() {
  groups.value = await api("environment-groups");
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
    await api("environment-groups", "POST", { ...form });
    form.code = "";
    form.name = "";
    await load();
    success.value = "Group created.";
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
  /><a-card title="Create environment group"
    ><p class="muted">
      Organize deployment environments under a platform group.
    </p>
    <form @submit.prevent="save">
      <div class="form-grid">
        <label
          >Group code<a-input
            v-model:value="form.code"
            required
            placeholder="raptor" /></label
        ><label
          >Group name<a-input
            v-model:value="form.name"
            required
            placeholder="Raptor"
        /></label>
      </div>
      <div class="actions">
        <a-button type="primary" html-type="submit" :loading="busy"
          >Create group</a-button
        >
      </div>
    </form></a-card
  ><a-card title="Groups" :loading="loading" class="section-space"
    ><table v-if="groups.length">
      <thead>
        <tr>
          <th>Code</th>
          <th>Name</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="group in groups" :key="group.code">
          <td>{{ group.code }}</td>
          <td>{{ group.name }}</td>
        </tr>
      </tbody>
    </table>
    <a-empty v-else description="No groups yet."
  /></a-card>
</template>

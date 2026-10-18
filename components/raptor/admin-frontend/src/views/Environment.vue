<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { api } from "../api";
const route = useRoute(),
  router = useRouter();
let active = true;
onUnmounted(() => {
  active = false;
});
const code = String(route.params.code || "");
const editing = route.path.endsWith("/edit"),
  creating = !code;
const fields = [
  ["code", "Code"],
  ["groupCode", "Group code"],
  ["stage", "Stage"],
  ["cloud", "Cloud"],
  ["region", "Region"],
  ["accountId", "Cloud account ID"],
  ["ackClusterId", "ACK cluster ID"],
];
interface Repository {
  purpose: string;
  url: string;
  baseBranch: string;
  directory: string;
}
const form = reactive<{ [key: string]: any; repositories: Repository[] }>({
  code: "",
  groupCode: "",
  stage: "",
  cloud: "",
  region: "",
  accountId: "",
  ackClusterId: "",
  repositories: [] as Repository[],
});
const loaded = ref(creating);
const loading = ref(!creating),
  busy = ref(false),
  error = ref("");
const back = computed(() =>
  editing ? "/environments/" + encodeURIComponent(code) : "/environments",
);
onMounted(async () => {
  if (creating) return;
  try {
    const env = await api("environments/" + encodeURIComponent(code));
    if (!active) return;
    for (const [key] of fields) form[key] = env[key] || "";
    form.repositories = (env.repositories || []).map((r: Repository) => ({
      ...r,
    }));
    loaded.value = true;
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
});
function addRepository() {
  form.repositories.push({
    purpose: "terraform",
    url: "",
    baseBranch: "main",
    directory: ".",
  });
}
async function save() {
  busy.value = true;
  error.value = "";
  try {
    await api(
      "environments" + (editing ? "/" + encodeURIComponent(code) : ""),
      editing ? "PATCH" : "POST",
      {
        ...form,
        repositories: form.repositories.map((r: Repository) => ({
          purpose: r.purpose,
          url: r.url.trim(),
          baseBranch: r.baseBranch.trim(),
          directory: r.directory.trim(),
        })),
      },
    );
    if (active)
      await router.push("/environments/" + encodeURIComponent(form.code));
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <RouterLink :to="back">← Environments</RouterLink
  ><a-alert v-if="error" :message="error" type="error" show-icon /><a-card
    class="section-space"
    :title="creating ? 'Create environment' : editing ? 'Edit ' + code : code"
    :loading="loading"
    ><template v-if="!creating && !editing" #extra
      ><RouterLink :to="'/environments/' + encodeURIComponent(code) + '/edit'"
        ><a-button type="primary">Edit</a-button></RouterLink
      ></template
    >
    <form v-if="loaded && (creating || editing)" @submit.prevent="save">
      <div class="form-grid">
        <label v-for="[key, label] in fields" :key="key"
          >{{ label
          }}<a-input
            v-model:value="form[key]"
            :required="['code', 'groupCode', 'stage'].includes(key)"
            :readonly="key === 'code' && editing"
        /></label>
      </div>
      <h3 class="section-space">Repositories</h3>
      <p class="muted">
        All configured repositories are attached to each sandbox attempt. Rows
        using the same repository URL must use the same base branch.
      </p>
      <fieldset
        v-for="(repo, index) in form.repositories"
        :key="index"
        class="repository-row"
      >
        <legend>Repository {{ index + 1 }}</legend>
        <div class="form-grid">
          <label
            >Purpose<a-select
              v-model:value="repo.purpose"
              :options="[
                { value: 'terraform', label: 'Terraform' },
                { value: 'kubernetes', label: 'Kubernetes' },
              ]" /></label
          ><label
            >HTTPS GitHub URL<a-input
              v-model:value="repo.url"
              type="url"
              required /></label
          ><label
            >Base branch<a-input
              v-model:value="repo.baseBranch"
              :required="creating || !!repo.baseBranch" /></label
          ><label
            >Directory<a-input v-model:value="repo.directory" required
          /></label>
        </div>
        <div class="actions">
          <a-button danger @click="form.repositories.splice(index, 1)"
            >Remove repository</a-button
          >
        </div>
      </fieldset>
      <div class="actions">
        <a-button @click="addRepository">Add repository</a-button>
      </div>
      <div class="actions">
        <a-button type="primary" html-type="submit" :loading="busy">{{
          editing ? "Save changes" : "Create environment"
        }}</a-button
        ><RouterLink :to="back">Cancel</RouterLink>
      </div>
    </form>
    <template v-else-if="loaded"
      ><dl class="details">
        <div
          v-for="[key, label] in fields.filter(([key]) => key !== 'code')"
          :key="key"
        >
          <dt>{{ label }}</dt>
          <dd>{{ form[key] || "—" }}</dd>
        </div>
      </dl>
      <h3 class="section-space">Repositories</h3>
      <div
        v-for="(repo, index) in form.repositories"
        :key="index"
        class="repository-row"
      >
        <dl class="details">
          <div
            v-for="[key, label] in [
              ['purpose', 'Purpose'],
              ['url', 'HTTPS GitHub URL'],
              ['baseBranch', 'Base branch'],
              ['directory', 'Directory'],
            ]"
            :key="key"
          >
            <dt>{{ label }}</dt>
            <dd>{{ repo[key] || "—" }}</dd>
          </div>
        </dl>
      </div>
      <a-empty
        v-if="!form.repositories.length"
        description="No repositories configured." /></template
  ></a-card>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute } from "vue-router";
import { api } from "../api";
import Environment from "./Environment.vue";
const route = useRoute();
const environments = ref<any[]>([]),
  search = ref(""),
  loading = ref(true),
  error = ref("");
const filtered = computed(() =>
  environments.value.filter((env) =>
    [env.code, env.groupCode, env.stage, env.cloud, env.region].some((v) =>
      String(v || "")
        .toLowerCase()
        .includes(search.value.trim().toLowerCase()),
    ),
  ),
);
onMounted(async () => {
  if (route.query.mode === "create") return;
  try {
    environments.value = await api("environments");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
});
</script>
<template>
  <Environment v-if="route.query.mode === 'create'" /><template v-else
    ><a-alert v-if="error" :message="error" type="error" show-icon /><a-card
      title="Configured environments"
      :loading="loading"
      ><template #extra
        ><RouterLink to="/environments?mode=create"
          ><a-button type="primary">Create environment</a-button></RouterLink
        ></template
      ><label style="max-width: 500px; margin-bottom: 24px"
        >Search environments<a-input-search
          v-model:value="search"
          placeholder="Code, group, stage, cloud or region"
      /></label>
      <div class="table-scroll" v-if="filtered.length">
        <table>
          <thead>
            <tr>
              <th>Environment</th>
              <th>Group</th>
              <th>Stage</th>
              <th>Cloud</th>
              <th>Region</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="env in filtered" :key="env.code">
              <td>
                <RouterLink
                  :to="'/environments/' + encodeURIComponent(env.code)"
                  >{{ env.code }}</RouterLink
                >
              </td>
              <td>{{ env.groupCode }}</td>
              <td>{{ env.stage }}</td>
              <td>{{ env.cloud || "—" }}</td>
              <td>{{ env.region || "—" }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <a-empty
        v-else
        :description="
          search ? 'No matching environments.' : 'No environments yet.'
        " /></a-card
  ></template>
</template>

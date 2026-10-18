import { createRouter, createWebHistory } from "vue-router";
import Users from "./views/Users.vue";
import Groups from "./views/Groups.vue";
import Environments from "./views/Environments.vue";
import Environment from "./views/Environment.vue";
import GitHubToken from "./views/GitHubToken.vue";
import Models from "./views/Models.vue";
export const menu = [
  { path: "/users", title: "Users" },
  { path: "/environment-groups", title: "Environment groups" },
  { path: "/environments", title: "Environments" },
  { path: "/ai-models", title: "AI models" },
  { path: "/github-pat", title: "GitHub token" },
];
export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: "/",
      redirect: (to) => ({
        path:
          (
            {
              "#users-section": "/users",
              "#groups-section": "/environment-groups",
              "#environments-section": "/environments",
              "#model-admin": "/ai-models",
            } as Record<string, string>
          )[to.hash] || "/users",
        hash: "",
      }),
    },
    { path: "/users", component: Users, meta: { title: "Users" } },
    {
      path: "/environment-groups",
      component: Groups,
      meta: { title: "Environment groups" },
    },
    {
      path: "/environments",
      component: Environments,
      meta: { title: "Environments" },
    },
    {
      path: "/environments/:code",
      component: Environment,
      meta: { title: "Environment details" },
    },
    {
      path: "/environments/:code/edit",
      component: Environment,
      meta: { title: "Edit environment" },
    },
    { path: "/ai-models", component: Models, meta: { title: "AI models" } },
    {
      path: "/github-pat",
      component: GitHubToken,
      meta: { title: "GitHub token" },
    },
  ],
});

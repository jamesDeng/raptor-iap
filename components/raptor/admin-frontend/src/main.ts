import { addIcon } from "@vben-core/icons";
import { createApp } from "vue";
import {
  Alert,
  Button,
  Card,
  ConfigProvider,
  Empty,
  Input,
  Menu,
  Select,
  Spin,
  Tag,
} from "ant-design-vue";
import "@vben-core/design";
import "ant-design-vue/dist/reset.css";
import "./style.css";
import App from "./App.vue";
import { router } from "./router";
// Register the two Vben mobile toggles locally; no external icon service is needed.
for (const name of ["ep:expand", "ep:fold"])
  addIcon(name, {
    width: 24,
    height: 24,
    body: '<path d="M4 6h16M4 12h16M4 18h16" fill="none" stroke="currentColor" stroke-width="2"/>',
  });
const app = createApp(App);
for (const component of [
  Alert,
  Button,
  Card,
  ConfigProvider,
  Empty,
  Input,
  Menu,
  Select,
  Spin,
  Tag,
])
  app.use(component);
app.use(router).mount("#app");

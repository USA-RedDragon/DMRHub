import { createApp } from "vue";
import { createPinia } from "pinia";

import PrimeVue from "primevue/config";
import ToastService from "primevue/toastservice";
import DialogService from "primevue/dialogservice";
import ConfirmationService from "primevue/confirmationservice";
import Toast from "primevue/toast";
import ConfirmDialog from "primevue/confirmdialog";

import App from "./App.vue";
import router from "./router";

import "primeicons/primeicons.css";
import "primevue/resources/themes/mdc-dark-indigo/theme.css";
import "primevue/resources/primevue.min.css";

import "./assets/main.css";

const pinia = createPinia();
const app = createApp(App);

app.use(ToastService);
app.use(DialogService);
app.use(ConfirmationService);
app.use(pinia);
app.use(PrimeVue);
app.use(router);

app.component("Toast", Toast);
app.component("ConfirmDialog", ConfirmDialog);

app.mount("#app");

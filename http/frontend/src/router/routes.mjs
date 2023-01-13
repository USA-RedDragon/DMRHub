export default [
  {
    path: "/",
    name: "Main",
    component: () => import("../views/MainPage.vue"),
  },
  {
    path: "/login",
    name: "Login",
    component: () => import("../views/LoginPage.vue"),
  },
];

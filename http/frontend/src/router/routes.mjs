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
  {
    path: "/register",
    name: "Register",
    component: () => import("../views/RegisterPage.vue"),
  },
  {
    path: "/repeaters",
    name: "Repeaters",
    component: () => import("../views/RepeatersPage.vue"),
  },
  {
    path: "/repeaters/new",
    name: "NewRepeater",
    component: () => import("../views/NewRepeaterPage.vue"),
  },
  {
    path: "/talkgroups",
    name: "Talkgroups",
    component: () => import("../views/TalkgroupsPage.vue"),
  },
  {
    path: "/settings",
    name: "Settings",
    component: () => import("../views/SettingsPage.vue"),
  },
  {
    path: "/admin/repeaters",
    name: "AdminRepeaters",
    component: () => import("../views/admin/RepeatersPage.vue"),
  },
  {
    path: "/admin/talkgroups",
    name: "AdminTalkgroups",
    component: () => import("../views/admin/TalkgroupsPage.vue"),
  },
  {
    path: "/admin/users",
    name: "AdminUsers",
    component: () => import("../views/admin/UsersPage.vue"),
  },
  {
    path: "/admin/users/approval",
    name: "AdminUsersApproval",
    component: () => import("../views/admin/UsersApprovalPage.vue"),
  },
];

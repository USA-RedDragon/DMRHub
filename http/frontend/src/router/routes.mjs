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
    path: "/nets",
    name: "Nets",
    component: () => import("../views/NetsPage.vue"),
  },
  {
    path: "/nets/my",
    name: "MyNets",
    component: () => import("../views/MyNetsPage.vue"),
  },
  {
    path: "/nets/manage",
    name: "ManageNets",
    component: () => import("../views/NetsManagePage.vue"),
  },
  {
    path: "/talkgroups/owned",
    name: "OwnedTalkgroups",
    component: () => import("../views/OwnedTalkgroupsPage.vue"),
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
    path: "/admin/talkgroups/new",
    name: "NewTalkgroups",
    component: () => import("../views/admin/NewTalkgroupsPage.vue"),
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

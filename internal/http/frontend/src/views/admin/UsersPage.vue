<template>
  <div>
    <Toast />
    <ConfirmDialog></ConfirmDialog>
    <Card>
      <template #title>Users</template>
      <template #content>
        <UserTable />
      </template>
    </Card>
  </div>
</template>

<script>
import Card from "primevue/card/sfc";
import UserTable from "@/components/UserTable.vue";

export default {
  components: {
    Card,
    UserTable,
  },
  created() {},
  mounted() {},
  unmounted() {},
  data: function () {
    return {};
  },
  methods: {},
  computed: {},
};
</script>

<style scoped></style>

<template>
  <div>
    <Toast />
    <ConfirmDialog></ConfirmDialog>
    <Card>
      <template #title>User Approvals</template>
      <template #content>
        <UserTable approval />
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

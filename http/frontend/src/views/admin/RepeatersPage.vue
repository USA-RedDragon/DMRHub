<template>
  <div>
    <Toast />
    <ConfirmDialog></ConfirmDialog>
    <Card>
      <template #title>Repeaters</template>
      <template #content>
        <RepeaterTable admin />
      </template>
    </Card>
  </div>
</template>

<script>
import Card from "primevue/card/sfc";

import RepeaterTable from "@/components/RepeaterTable.vue";

export default {
  components: {
    Card,
    RepeaterTable,
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

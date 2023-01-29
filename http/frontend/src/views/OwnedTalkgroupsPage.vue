<template>
  <div>
    <Toast />
    <ConfirmDialog></ConfirmDialog>
    <Card>
      <template #title>Owned Talkgroups</template>
      <template #content>
        <TalkgroupTable owner />
      </template>
    </Card>
  </div>
</template>

<script>
import Card from "primevue/card/sfc";

import TalkgroupTable from "@/components/TalkgroupTable.vue";

export default {
  components: {
    Card,
    TalkgroupTable,
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

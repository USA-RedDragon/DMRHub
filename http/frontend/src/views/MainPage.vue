<template>
  <div>
    <Card>
      <template #title>Last Heard</template>
      <template #content>
        <LastHeardTable />
      </template>
    </Card>
  </div>
</template>

<script>
import Card from "primevue/card/sfc";
import LastHeardTable from "@/components/LastHeardTable.vue";

export default {
  components: {
    Card,
    LastHeardTable,
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

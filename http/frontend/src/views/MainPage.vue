<template>
  <div>
    <Card>
      <template #title>Last Heard</template>
      <template #content>
        <!-- <DataTable :value="talkgroups">
          <Column field="id" header="Channel"></Column>
          <Column field="name" header="Name"></Column>
          <Column field="description" header="Description"></Column>
          <Column field="admins" header="Admins"></Column>
          <Column field="created_at" header="Created At"></Column>
        </DataTable> -->
        Last Heard coming soon :)<br />
        This site is a work in progress. Please report any issues to KI5VMF.
      </template>
    </Card>
  </div>
</template>

<script>
import Card from "primevue/card/sfc";

export default {
  components: {
    Card,
  },
  created() {},
  mounted() {},
  data: function () {
    return {};
  },
  methods: {},
};
</script>

<style scoped></style>

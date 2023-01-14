<template>
  <div>
    <h1>Admin Repeaters</h1>
    <div class="content">
      <Card>
        <template #header>
          <p>Header</p>
        </template>
        <template #title> Advanced Card </template>
        <template #content>
          Lorem ipsum dolor sit amet, consectetur adipisicing elit. Inventore
          sed consequuntur error repudiandae numquam deserunt quisquam repellat
          libero asperiores earum nam nobis, culpa ratione quam perferendis
          esse, cupiditate neque quas!
        </template>
        <template #footer>
          <p>Footer</p>
        </template>
      </Card>
    </div>
  </div>
</template>

<script>
import Card from "primevue/card/sfc";

export default {
  components: {},
  created() {},
  mounted() {},
  data: function () {
    return {};
  },
  methods: {},
};
</script>

<style scoped></style>

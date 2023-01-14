<template>
  <Header />
  <RouterView />
  <Footer />
</template>

<script>
import { RouterView } from "vue-router";
import Footer from "./components/Footer.vue";
import Header from "./components/Header.vue";

export default {
  name: 'App',
  components: {
    Header,
    Footer
  },
  data() {
    return {
      loggedIn: false,
      // localStorage in Firefox is string-only
      dark: localStorage.dark === 'true' ? true : false,
      user: {},
    };
  },
  watch: {
    dark(_newValue) {
      // localStorage in Firefox is string-only
      localStorage.dark = this.dark ? 'true' : 'false';
    },
  },
  mounted() {

  },
};
</script>

<style scoped>

</style>

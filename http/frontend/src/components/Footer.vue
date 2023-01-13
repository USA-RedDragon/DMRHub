<template>
  <div class="footer">
    <p>
      This project proudly open
      <a href="https://github.com/USA-RedDragon/dmrserver-in-a-box">source</a>
    </p>
    <p>Copyright &copy; 2023-{{ year }} Jacob McSwain</p>
    <p id="version">Version {{ version }}</p>
  </div>
</template>

<script>
import API from "@/services/API";
export default {
  name: "SiteFooter",
  data: function () {
    return {
      year: new Date().getFullYear(),
      version: "",
    };
  },
  // Get version from API onMounted
  mounted() {
    API.get("/version")
      .then((response) => {
        this.version = response.data;
      })
      .catch((error) => {
        console.log(error);
      });
  },
};
</script>

<style scoped>
.footer {
  text-align: center;
  display: block;
  padding: 1em;
  padding-bottom: 0;
  margin-bottom: 0;
  font-size: 0.8em;
}

.footer p#version {
  font-size: 0.8em;
  margin-top: 0;
  color: #666;
}
</style>

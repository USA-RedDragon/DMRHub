<template>
  <div>
    <Toast />
    <ConfirmDialog>
      <template #message="slotProps">
        <div class="flex p-4">
          <p>
            You will need to use this DMRGateway configuration to connect to the
            network.
            <span style="color: red"
              >Save this now, as you will not be able to retrieve it
              again.</span
            >
            <br /><br />
          </p>
          <pre style="background-color: #444; padding: 1em">
[DMR Network 2]
Name=AREDN
Enabled=1
Address=ki5vmf-server.local.mesh
Port=62031
Password="{{ slotProps.message.message }}"
Id={{ this.radio_id }}
Location=1
Debug=0
# Rewrites TG 8000001-8999999 -> 1-999999
PCRewrite1=1,8009990,1,9990,1
PCRewrite2=2,8009990,2,9990,1
TypeRewrite1=1,8009990,1,9990
TypeRewrite2=2,8009990,2,9990
TGRewrite1=1,8000001,1,1,999999
TGRewrite2=2,8000001,2,1,999999
SrcRewrite1=1,9990,1,8009990,1
SrcRewrite2=2,9990,2,8009990,1
SrcRewrite3=1,1,1,8000001,999999
SrcRewrite4=2,1,2,8000001,999999</pre
          >
        </div>
      </template>
    </ConfirmDialog>
    <Card>
      <template #title>New Repeater</template>
      <template #content>
        <span class="p-float-label">
          <InputText id="radio_id" type="text" v-model="radio_id" />
          <label for="radio_id">DMR Radio ID</label>
        </span>
      </template>
      <template #footer>
        <div class="card-footer">
          <Button
            class="p-button-raised p-button-rounded"
            icon="pi pi-save"
            label="Save"
            @click="handleRepeater()"
          />
        </div>
      </template>
    </Card>
  </div>
</template>

<script>
import Card from "primevue/card/sfc";
import Checkbox from "primevue/checkbox/sfc";
import Button from "primevue/button/sfc";
import InputText from "primevue/inputtext/sfc";
import API from "@/services/API";

export default {
  components: {
    Card,
    Checkbox,
    Button,
    InputText,
  },
  created() {},
  mounted() {},
  data: function () {
    return {
      radio_id: "",
    };
  },
  methods: {
    handleRepeater() {
      var numericID = parseInt(this.radio_id);
      if (!numericID) {
        return;
      }
      API.post("/repeaters", {
        id: numericID,
        password: this.repeater_password,
      })
        .then((res) => {
          if (!res.data) {
            this.$toast.add({
              summary: "Error",
              severity: "error",
              detail: `Error deletng repeater`,
              life: 3000,
            });
          } else {
            this.$confirm.require({
              message: res.data.password,
              header: "Repeater Created",
              acceptClass: "p-button-success",
              rejectClass: "remove-reject-button",
              acceptLabel: "OK",
              accept: () => {
                this.$router.push("/repeaters");
              },
            });
          }
        })
        .catch((err) => {
          console.error(err);
        });
    },
  },
};
</script>

<style>
.remove-reject-button,
.p-dialog-header-close {
  display: none !important;
}
</style>

<style scoped></style>

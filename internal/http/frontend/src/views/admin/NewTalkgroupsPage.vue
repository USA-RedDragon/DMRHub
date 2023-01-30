<template>
  <div>
    <Toast />
    <form @submit.prevent="handleTalkgroup(!v$.$invalid)">
      <Card>
        <template #title>New Talkgroup</template>
        <template #content>
          <span class="p-float-label">
            <InputText
              id="id"
              type="text"
              v-model="v$.id.$model"
              :class="{
                'p-invalid': v$.id.$invalid && submitted,
              }"
            />
            <label for="id" :class="{ 'p-error': v$.id.$invalid && submitted }"
              >Talkgroup ID</label
            >
          </span>
          <span v-if="v$.id.$error && submitted">
            <span v-for="(error, index) of v$.id.$errors" :key="index">
              <small class="p-error">{{ error.$message }}</small>
            </span>
          </span>
          <span v-else>
            <small
              v-if="(v$.id.$invalid && submitted) || v$.id.$pending.$response"
              class="p-error"
              >{{ v$.id.required.$message.replace("Value", "ID") }}
              <br />
            </small>
          </span>
          <br />
          <span class="p-float-label">
            <InputText
              id="name"
              type="text"
              v-model="v$.name.$model"
              :class="{
                'p-invalid': v$.name.$invalid && submitted,
              }"
            />
            <label
              for="name"
              :class="{ 'p-error': v$.name.$invalid && submitted }"
              >Name</label
            >
          </span>
          <span v-if="v$.name.$error && submitted">
            <span v-for="(error, index) of v$.name.$errors" :key="index">
              <small class="p-error">{{ error.$message }}</small>
              <br />
            </span>
          </span>
          <span v-else>
            <small
              v-if="
                (v$.name.$invalid && submitted) || v$.name.$pending.$response
              "
              class="p-error"
              >{{ v$.name.required.$message.replace("Value", "Name") }}
              <br />
            </small>
          </span>
          <br />
          <span class="p-float-label">
            <InputText
              id="description"
              type="text"
              v-model="v$.description.$model"
              :class="{
                'p-invalid': v$.description.$invalid && submitted,
              }"
            />
            <label
              for="description"
              :class="{ 'p-error': v$.description.$invalid && submitted }"
              >Description</label
            >
          </span>
          <span v-if="v$.description.$error && submitted">
            <span v-for="(error, index) of v$.description.$errors" :key="index">
              <small class="p-error">{{ error.$message }}</small>
            </span>
            <br />
          </span>
          <span v-else>
            <small
              v-if="
                (v$.description.$invalid && submitted) ||
                v$.description.$pending.$response
              "
              class="p-error"
              >{{
                v$.description.required.$message.replace("Value", "Description")
              }}
              <br />
            </small>
          </span>
          <br />
          <span class="p-float-label">
            <MultiSelect
              id="admins"
              v-model="admins"
              :options="allUsers"
              :filter="true"
              optionLabel="display"
              display="chip"
              style="width: 100%"
            >
              <template #chip="slotProps">
                {{ slotProps.value.display }}
              </template>
              <template #option="slotProps">
                {{ slotProps.option.display }}
              </template>
            </MultiSelect>
            <label for="admins">Admins</label>
          </span>
          <br />
          <span class="p-float-label">
            <MultiSelect
              id="ncos"
              v-model="ncos"
              :options="allUsers"
              :filter="true"
              optionLabel="display"
              display="chip"
              style="width: 100%"
            >
              <template #chip="slotProps">
                {{ slotProps.value.display }}
              </template>
              <template #option="slotProps">
                {{ slotProps.option.display }}
              </template>
            </MultiSelect>
            <label for="ncos">Net Control Operators</label>
          </span>
        </template>
        <template #footer>
          <div class="card-footer">
            <Button
              class="p-button-raised p-button-rounded"
              icon="pi pi-save"
              label="Save"
              type="submit"
              @click="handleTalkgroup(!v$.$invalid)"
            />
          </div>
        </template>
      </Card>
    </form>
  </div>
</template>

<script>
import Card from "primevue/card/sfc";
import Checkbox from "primevue/checkbox/sfc";
import Button from "primevue/button/sfc";
import InputText from "primevue/inputtext/sfc";
import MultiSelect from "primevue/multiselect/sfc";
import API from "@/services/API";
import { useVuelidate } from "@vuelidate/core";
import { required, numeric, maxLength } from "@vuelidate/validators";

export default {
  components: {
    Card,
    Checkbox,
    Button,
    InputText,
    MultiSelect,
  },
  setup: () => ({ v$: useVuelidate() }),
  created() {},
  mounted() {
    this.getData();
  },
  data: function () {
    return {
      id: "",
      name: "",
      description: "",
      admins: [],
      ncos: [],
      allUsers: [],
      submitted: false,
    };
  },
  validations() {
    return {
      id: {
        required,
        numeric,
      },
      name: {
        required,
        maxLength: maxLength(20),
      },
      description: {
        required,
        maxLength: maxLength(240),
      },
      ncos: {},
      admins: {},
    };
  },
  methods: {
    getData() {
      API.get("/users?limit=none")
        .then((res) => {
          this.allUsers = res.data.users;
          var parrotIndex = -1;
          for (let i = 0; i < this.allUsers.length; i++) {
            this.allUsers[
              i
            ].display = `${this.allUsers[i].id} - ${this.allUsers[i].callsign}`;
            // Remove user with id 9990 (parrot)
            if (this.allUsers[i].id === 9990) {
              parrotIndex = i;
            }
          }
          if (parrotIndex !== -1) {
            this.allUsers.splice(parrotIndex, 1);
          }
        })
        .catch((err) => {
          console.error(err);
        });
    },
    handleTalkgroup(isFormValid) {
      this.submitted = true;

      if (!isFormValid) {
        return;
      }

      var numericID = parseInt(this.id);
      if (!numericID) {
        return;
      }
      API.post("/talkgroups", {
        id: numericID,
        name: this.name,
        description: this.description,
      })
        .then((_res) => {
          API.post(`/talkgroups/${numericID}/admins`, {
            user_ids: this.admins.map((admin) => admin.id),
          })
            .then(() => {
              API.post(`/talkgroups/${numericID}/ncos`, {
                user_ids: this.ncos.map((nco) => nco.id),
              })
                .then(() => {
                  // Now show a toast for a few seconds before redirecting to /admin/talkgroups
                  this.$toast.add({
                    summary: "Success",
                    severity: "success",
                    detail: `Talkgroup created, redirecting...`,
                    life: 3000,
                  });
                  setTimeout(() => {
                    this.$router.push("/admin/talkgroups");
                  }, 3000);
                })
                .catch((err) => {
                  console.error(err);
                  if (
                    err.response &&
                    err.response.data &&
                    err.response.data.error
                  ) {
                    this.$toast.add({
                      summary: "Error",
                      severity: "error",
                      detail: err.response.data.error,
                      life: 3000,
                    });
                  } else {
                    this.$toast.add({
                      summary: "Error",
                      severity: "error",
                      detail: `Error creating talkgroup`,
                      life: 3000,
                    });
                  }
                });
            })
            .catch((err) => {
              console.error(err);
              if (
                err.response &&
                err.response.data &&
                err.response.data.error
              ) {
                this.$toast.add({
                  summary: "Error",
                  severity: "error",
                  detail: err.response.data.error,
                  life: 3000,
                });
              } else {
                this.$toast.add({
                  summary: "Error",
                  severity: "error",
                  detail: `Error creating talkgroup`,
                  life: 3000,
                });
              }
            });
        })
        .catch((err) => {
          console.error(err);
          if (err.response && err.response.data && err.response.data.error) {
            this.$toast.add({
              summary: "Error",
              severity: "error",
              detail: err.response.data.error,
              life: 3000,
            });
          } else {
            this.$toast.add({
              summary: "Error",
              severity: "error",
              detail: `Error creating talkgroup`,
              life: 3000,
            });
          }
        });
    },
  },
};
</script>

<style scoped></style>

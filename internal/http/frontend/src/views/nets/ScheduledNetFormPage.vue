<!--
  SPDX-License-Identifier: AGPL-3.0-or-later
  DMRHub - Run a DMR network server in a single binary
  Copyright (C) 2023-2026 Jacob McSwain

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published by
  the Free Software Foundation, either version 3 of the License, or
  (at your option) any later version.

  This program is distributed in the hope that it will be useful,
  but WITHOUT ANY WARRANTY; without even the implied warranty of
  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
  GNU Affero General Public License for more details.

  You should have received a copy of the GNU Affero General Public License
  along with this program. If not, see <https://www.gnu.org/licenses/>.

  The source code is available at <https://github.com/USA-RedDragon/DMRHub>
-->

<template>
  <div>
    <form @submit.prevent="handleSubmit(!v$.$invalid)">
      <Card>
        <CardHeader>
          <CardTitle>{{ isEdit ? 'Edit Scheduled Net' : 'New Scheduled Net' }}</CardTitle>
          <CardDescription>
            Times are entered in your local timezone and converted to UTC for storage.
          </CardDescription>
        </CardHeader>
        <CardContent class="space-y-4">
          <!-- Talkgroup (only on create) -->
          <div v-if="!isEdit">
            <label class="field-label" for="talkgroup_id">Talkgroup</label>
            <ShadSelect v-model="talkgroupValue" @update:model-value="onTalkgroupSelect">
              <SelectTrigger id="talkgroup_id">
                <SelectValue placeholder="Select a talkgroup" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="tg in talkgroups"
                  :key="tg.id"
                  :value="String(tg.id)"
                >
                  TG {{ tg.id }} — {{ tg.name }}
                </SelectItem>
              </SelectContent>
            </ShadSelect>
            <span v-if="v$.talkgroup_id.$error && submitted">
              <span v-for="(error, index) of v$.talkgroup_id.$errors" :key="index">
                <small class="p-error">{{ error.$message }}</small>
              </span>
            </span>
          </div>

          <!-- Name -->
          <div>
            <label class="field-label" for="name">Name</label>
            <ShadInput
              id="name"
              type="text"
              v-model="v$.name.$model"
              :aria-invalid="v$.name.$invalid && submitted"
              placeholder="Weekly net"
            />
            <span v-if="v$.name.$error && submitted">
              <span v-for="(error, index) of v$.name.$errors" :key="index">
                <small class="p-error">{{ error.$message.replace('Value', 'Name') }}</small>
              </span>
            </span>
          </div>

          <!-- Description -->
          <div>
            <label class="field-label" for="description">Description (optional)</label>
            <ShadInput
              id="description"
              type="text"
              v-model="description"
              placeholder="Weekly check-in net for club members"
            />
          </div>

          <!-- Day of Week -->
          <div>
            <label class="field-label" for="day_of_week">Day of Week (your local time)</label>
            <ShadSelect v-model="dayOfWeekLocal">
              <SelectTrigger id="day_of_week">
                <SelectValue placeholder="Select a day" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="day in dayOptions"
                  :key="day.value"
                  :value="String(day.value)"
                >
                  {{ day.label }}
                </SelectItem>
              </SelectContent>
            </ShadSelect>
            <span v-if="v$.dayOfWeekLocal.$error && submitted">
              <span v-for="(error, index) of v$.dayOfWeekLocal.$errors" :key="index">
                <small class="p-error">{{ error.$message }}</small>
              </span>
            </span>
          </div>

          <!-- Time of Day (local) -->
          <div>
            <label class="field-label" for="time_of_day">Time (your local time)</label>
            <ShadInput
              id="time_of_day"
              type="time"
              v-model="v$.timeOfDayLocal.$model"
              :aria-invalid="v$.timeOfDayLocal.$invalid && submitted"
            />
            <span v-if="v$.timeOfDayLocal.$error && submitted">
              <span v-for="(error, index) of v$.timeOfDayLocal.$errors" :key="index">
                <small class="p-error">{{ error.$message.replace('Value', 'Time') }}</small>
              </span>
            </span>
          </div>

          <!-- Duration -->
          <div>
            <label class="field-label" for="duration_minutes">Duration (minutes, optional)</label>
            <ShadInput
              id="duration_minutes"
              type="number"
              v-model="duration_minutes"
              placeholder="60"
              min="1"
              max="1440"
            />
          </div>

          <!-- Enabled -->
          <div class="flex items-center gap-2">
            <Checkbox
              id="enabled"
              :checked="enabled"
              @update:checked="(val: boolean) => { enabled = val; }"
            />
            <label for="enabled" class="text-sm font-medium">Enabled</label>
          </div>

          <!-- UTC preview -->
          <div v-if="utcPreview" class="text-sm text-muted-foreground">
            Will run at <strong>{{ utcPreview }}</strong> UTC every
            <strong>{{ utcDayPreview }}</strong>
          </div>
        </CardContent>
        <CardFooter>
          <div class="card-footer">
            <ShadButton type="submit" variant="outline" size="sm">
              {{ isEdit ? 'Update' : 'Create' }}
            </ShadButton>
          </div>
        </CardFooter>
      </Card>
    </form>
  </div>
</template>

<script lang="ts">
import { Button as ShadButton } from '@/components/ui/button';
import { Input as ShadInput } from '@/components/ui/input';
import { Checkbox } from '@/components/ui/checkbox';
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import {
  Select as ShadSelect,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import API from '@/services/API';
import {
  createScheduledNet,
  getScheduledNet,
  updateScheduledNet,
  type ScheduledNet,
} from '@/services/net';
import { localToUTC, utcToLocal } from '@/lib/timeConversion';
import { useVuelidate } from '@vuelidate/core';
import { required } from '@vuelidate/validators';

const dayNames = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];

type TalkgroupOption = {
  id: number;
  name: string;
};

export default {
  components: {
    ShadButton,
    ShadInput,
    Checkbox,
    Card,
    CardContent,
    CardDescription,
    CardFooter,
    CardHeader,
    CardTitle,
    ShadSelect,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
  },
  head: {
    title: 'Scheduled Net',
  },
  setup: () => ({ v$: useVuelidate() }),
  mounted() {
    this.fetchTalkgroups();
    const id = this.$route.params.id;
    if (id) {
      this.editID = Number(id);
      this.loadExisting();
    }
    // Pre-select talkgroup when navigating from a talkgroup page
    const tgQuery = this.$route.query.talkgroup_id;
    if (tgQuery && !this.editID) {
      const tgID = Number(tgQuery);
      if (!isNaN(tgID) && tgID > 0) {
        this.talkgroup_id = tgID;
        this.talkgroupValue = String(tgID);
      }
    }
  },
  data() {
    return {
      editID: null as number | null,
      talkgroup_id: null as number | null,
      talkgroupValue: '' as string,
      name: '',
      description: '',
      dayOfWeekLocal: '' as string,
      timeOfDayLocal: '',
      duration_minutes: '' as string | number,
      enabled: true,
      talkgroups: [] as TalkgroupOption[],
      submitted: false,
      dayOptions: dayNames.map((label, value) => ({ label, value })),
    };
  },
  computed: {
    isEdit(): boolean {
      return this.editID !== null;
    },
    utcPreview(): string | null {
      if (this.dayOfWeekLocal === '' || !this.timeOfDayLocal) return null;
      const { timeOfDay } = localToUTC(Number(this.dayOfWeekLocal), this.timeOfDayLocal);
      return timeOfDay;
    },
    utcDayPreview(): string | null {
      if (this.dayOfWeekLocal === '' || !this.timeOfDayLocal) return null;
      const { dayOfWeek } = localToUTC(Number(this.dayOfWeekLocal), this.timeOfDayLocal);
      return dayNames[dayOfWeek] ?? null;
    },
  },
  validations() {
    return {
      talkgroup_id: this.isEdit ? {} : { required },
      name: { required },
      dayOfWeekLocal: { required },
      timeOfDayLocal: { required },
    };
  },
  methods: {
    fetchTalkgroups() {
      API.get('/talkgroups?limit=none')
        .then((res) => {
          this.talkgroups = (res.data.talkgroups || []).map(
            (tg: { id: number; name: string }) => ({ id: tg.id, name: tg.name }),
          );
        })
        .catch((err) => console.error(err));
    },
    loadExisting() {
      if (!this.editID) return;
      getScheduledNet(this.editID)
        .then((res) => {
          const sn: ScheduledNet = res.data;
          this.talkgroup_id = sn.talkgroup_id;
          this.name = sn.name;
          this.description = sn.description || '';
          this.enabled = sn.enabled;
          this.duration_minutes = sn.duration_minutes ?? '';

          // Convert stored UTC values back to local for the form
          const local = utcToLocal(sn.day_of_week, sn.time_of_day);
          this.dayOfWeekLocal = String(local.dayOfWeek);
          this.timeOfDayLocal = local.timeOfDay;
        })
        .catch((err) => {
          console.error(err);
          this.$toast.add({
            summary: 'Error',
            severity: 'error',
            detail: 'Failed to load scheduled net',
            life: 3000,
          });
        });
    },
    onTalkgroupSelect(val: string | number | bigint | boolean | Record<string, string> | null | undefined) {
      this.talkgroup_id = Number(val);
    },
    handleSubmit(isFormValid: boolean) {
      this.submitted = true;
      if (!isFormValid) return;

      // Convert local day/time to UTC
      const utc = localToUTC(Number(this.dayOfWeekLocal), this.timeOfDayLocal);
      const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone;

      const dur = this.duration_minutes !== '' ? Number(this.duration_minutes) : undefined;

      if (this.isEdit && this.editID) {
        updateScheduledNet(this.editID, {
          name: this.name.trim(),
          description: this.description.trim() || undefined,
          day_of_week: utc.dayOfWeek,
          time_of_day: utc.timeOfDay,
          timezone,
          duration_minutes: dur,
          enabled: this.enabled,
        })
          .then(() => {
            this.$toast.add({
              summary: 'Success',
              severity: 'success',
              detail: 'Scheduled net updated',
              life: 3000,
            });
            setTimeout(() => this.$router.push('/nets'), 2000);
          })
          .catch((err) => {
            console.error(err);
            const msg = err?.response?.data?.error || 'Failed to update scheduled net';
            this.$toast.add({ summary: 'Error', severity: 'error', detail: msg, life: 3000 });
          });
      } else {
        if (!this.talkgroup_id) return;
        createScheduledNet({
          talkgroup_id: this.talkgroup_id,
          name: this.name.trim(),
          description: this.description.trim() || undefined,
          day_of_week: utc.dayOfWeek,
          time_of_day: utc.timeOfDay,
          timezone,
          duration_minutes: dur,
          enabled: this.enabled,
        })
          .then(() => {
            this.$toast.add({
              summary: 'Success',
              severity: 'success',
              detail: 'Scheduled net created',
              life: 3000,
            });
            setTimeout(() => this.$router.push('/nets'), 2000);
          })
          .catch((err) => {
            console.error(err);
            const msg = err?.response?.data?.error || 'Failed to create scheduled net';
            this.$toast.add({ summary: 'Error', severity: 'error', detail: msg, life: 3000 });
          });
      }
    },
  },
};
</script>

<style scoped>
.field-label {
  display: block;
  margin-bottom: 0.25rem;
  font-size: 0.875rem;
  font-weight: 500;
}

.card-footer {
  display: flex;
  justify-content: flex-end;
}
</style>

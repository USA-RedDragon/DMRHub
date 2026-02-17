<script setup lang="ts" generic="TData extends Record<string, unknown>">
import type { Table } from "@tanstack/vue-table"
import { Button as ShadButton } from "@/components/ui/button"

defineProps<{
  table: Table<TData>
}>()
</script>

<template>
  <div class="flex items-center justify-between gap-2">
    <div class="text-sm text-muted-foreground">
      Page {{ table.getState().pagination.pageIndex + 1 }}
      <span v-if="table.getPageCount() > 0">of {{ table.getPageCount() }}</span>
    </div>
    <div class="flex items-center gap-2">
      <ShadButton
        variant="outline"
        size="sm"
        :disabled="!table.getCanPreviousPage()"
        @click="table.previousPage()"
      >
        Previous
      </ShadButton>
      <ShadButton variant="outline" size="sm" :disabled="!table.getCanNextPage()" @click="table.nextPage()">
        Next
      </ShadButton>
    </div>
  </div>
</template>

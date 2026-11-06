<script setup lang="ts" generic="TData extends Record<string, unknown>">
import { Button as ShadButton } from "@/components/ui/button"
import type { DataTableInstance } from "./features"

defineProps<{
  table: DataTableInstance<TData>
  summary?: string
}>()
</script>

<template>
  <div class="flex flex-wrap items-center justify-between gap-2">
    <div class="text-sm text-muted-foreground">
      Page {{ table.atoms.pagination.get().pageIndex + 1 }}
      <span v-if="table.getPageCount() > 0">of {{ table.getPageCount() }}</span>
      <span v-if="summary"> · {{ summary }}</span>
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

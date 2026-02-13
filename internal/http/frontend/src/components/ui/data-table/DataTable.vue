<script setup lang="ts" generic="TData extends Record<string, unknown>">
import type {
  ColumnDef,
  PaginationState,
  SortingState,
  Updater,
} from "@tanstack/vue-table"
import {
  FlexRender,
  getCoreRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  useVueTable,
} from "@tanstack/vue-table"
import { computed, ref, watch } from "vue"

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { valueUpdater } from "@/components/ui/table/utils"
import DataTablePagination from "./DataTablePagination.vue"

const props = withDefaults(
  defineProps<{
    columns: ColumnDef<TData, unknown>[]
    data: TData[]
    loading?: boolean
    loadingText?: string
    emptyText?: string
    pageIndex?: number
    pageSize?: number
    pageCount?: number
    manualPagination?: boolean
  }>(),
  {
    loading: false,
    loadingText: "Loading...",
    emptyText: "No results.",
    pageIndex: 0,
    pageSize: 30,
    pageCount: -1,
    manualPagination: false,
  },
)

const emit = defineEmits<{
  (e: "update:pageIndex", value: number): void
  (e: "update:pageSize", value: number): void
}>()

const sorting = ref<SortingState>([])
const pagination = ref<PaginationState>({
  pageIndex: props.pageIndex ?? 0,
  pageSize: props.pageSize ?? 30,
})

watch(
  () => [props.pageIndex, props.pageSize],
  ([pageIndex, pageSize]) => {
    pagination.value = {
      pageIndex: pageIndex ?? 0,
      pageSize: pageSize ?? 30,
    }
  },
)

const table = useVueTable({
  get data() {
    return props.data
  },
  get columns() {
    return props.columns
  },
  get pageCount() {
    return props.pageCount
  },
  get manualPagination() {
    return props.manualPagination
  },
  getCoreRowModel: getCoreRowModel(),
  getPaginationRowModel: getPaginationRowModel(),
  getSortedRowModel: getSortedRowModel(),
  onSortingChange: (updaterOrValue: Updater<SortingState>) =>
    valueUpdater(updaterOrValue, sorting),
  onPaginationChange: (updaterOrValue: Updater<PaginationState>) => {
    const nextValue =
      typeof updaterOrValue === "function"
        ? updaterOrValue(pagination.value)
        : updaterOrValue

    pagination.value = nextValue
    emit("update:pageIndex", nextValue.pageIndex)
    emit("update:pageSize", nextValue.pageSize)
  },
  state: {
    get sorting() {
      return sorting.value
    },
    get pagination() {
      return pagination.value
    },
  },
})

const visibleRows = computed(() => {
  return table.getRowModel().rows
})
</script>

<template>
  <div class="space-y-4">
    <Table>
      <TableHeader>
        <TableRow v-for="headerGroup in table.getHeaderGroups()" :key="headerGroup.id">
          <TableHead v-for="header in headerGroup.headers" :key="header.id">
            <FlexRender
              v-if="!header.isPlaceholder"
              :render="header.column.columnDef.header"
              :props="header.getContext()"
            />
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-if="loading">
          <TableCell :colspan="columns.length" class="text-center">{{ loadingText }}</TableCell>
        </TableRow>
        <TableRow v-else-if="visibleRows.length === 0">
          <TableCell :colspan="columns.length" class="text-center">{{ emptyText }}</TableCell>
        </TableRow>
        <TableRow v-for="row in visibleRows" :key="row.id">
          <TableCell v-for="cell in row.getVisibleCells()" :key="cell.id">
            <FlexRender :render="cell.column.columnDef.cell" :props="cell.getContext()" />
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>

    <DataTablePagination :table="table" />
  </div>
</template>

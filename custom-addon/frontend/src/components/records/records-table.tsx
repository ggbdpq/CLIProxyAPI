import { useMemo } from 'react'

import { Checkbox } from '@/components/ui/checkbox'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { tableFeatures, useTable, type ColumnDef } from '@tanstack/react-table'
import { dataColumns, formatCellValue, type DataRecord } from '@/lib/schemas'
import { useFilters } from '@/store/app'

interface RecordsTableProps {
  records: DataRecord[]
  onDeleteOne: (id: number) => void
}

/** v9 特性集：只需 core（行模型自动），选中态由 Zustand 管理不注册 rowSelection */
const features = tableFeatures({})
const EMPTY_RECORDS: DataRecord[] = []

function recordDataObject(record: DataRecord): Record<string, unknown> {
  const data = record.data
  if (data && typeof data === 'object' && !Array.isArray(data)) return data as Record<string, unknown>
  return { value: data }
}

/** 动态列表格：列名从当前页 records 的 data 实时收集（1:1 对等旧版） */
export function RecordsTable({ records, onDeleteOne }: RecordsTableProps) {
  const selected = useFilters((s) => s.selected)
  const toggleSelected = useFilters((s) => s.toggleSelected)
  const selectAll = useFilters((s) => s.selectAll)

  const columns = useMemo<ColumnDef<typeof features, DataRecord>[]>(() => {
    const keys = dataColumns(records)
    const dynamic: ColumnDef<typeof features, DataRecord>[] = keys.map((key) => ({
      id: key,
      header: key,
      cell: ({ row }) => {
        const text = formatCellValue(recordDataObject(row.original)[key], key)
        return (
          <div className="max-w-[520px]">
            <pre className="font-mono overflow-hidden whitespace-pre-wrap break-words text-[13px] [display:-webkit-box] [-webkit-line-clamp:2] [-webkit-box-orient:vertical]">
              {text}
            </pre>
          </div>
        )
      },
    }))
    return [
      {
        id: '__select',
        header: () => {
          const ids = records.map((r) => String(r.id))
          const allChecked = ids.length > 0 && ids.every((id) => selected[id])
          return (
            <Checkbox
              aria-label="全选当前列表"
              checked={allChecked}
              onCheckedChange={(checked) => selectAll(ids, checked === true)}
            />
          )
        },
        cell: ({ row }) => (
          <Checkbox
            aria-label={`选择 ${row.original.id}`}
            checked={Boolean(selected[String(row.original.id)])}
            onCheckedChange={(checked) => toggleSelected(String(row.original.id), checked === true)}
          />
        ),
      },
      { id: 'id', header: 'ID', cell: ({ row }) => <span className="font-mono text-[13px]">{row.original.id}</span> },
      ...dynamic,
      {
        id: '__actions',
        header: '操作',
        cell: ({ row }) => (
          <Button
            type="button"
            variant="destructive"
            size="sm"
            onClick={() => onDeleteOne(row.original.id)}
          >
            删除
          </Button>
        ),
      },
    ]
  }, [records, selected, selectAll, toggleSelected, onDeleteOne])

  const table = useTable({
    features,
    columns,
    data: records.length ? records : EMPTY_RECORDS,
    getRowId: (row) => String(row.id),
  })

  if (!records.length) {
    return <div className="text-muted-foreground p-8 text-center">暂无数据</div>
  }

  return (
    <div className="overflow-auto rounded-xl border">
      <Table className="min-w-[860px]">
        <TableHeader>
          {table.getHeaderGroups().map((group) => (
            <TableRow key={group.id}>
              {group.headers.map((header) => (
                <TableHead key={header.id} className="whitespace-nowrap">
                  {header.isPlaceholder ? null : <table.FlexRender header={header} />}
                </TableHead>
              ))}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {table.getRowModel().rows.map((row) => (
            <TableRow key={row.id}>
              {row.getAllCells().map((cell) => (
                <TableCell key={cell.id} className="align-top">
                  <table.FlexRender cell={cell} />
                </TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

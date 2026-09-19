import { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Form, Input, Modal, Select, Table, Tag } from 'antd'
import { CheckCircle2, Play, RefreshCw, Search, Send, ShieldAlert, XCircle } from 'lucide-react'
import { listMeasurements } from '../api/measurements'
import { EvidenceBreakdownPanel } from '../components/common/EvidenceBreakdownPanel'
import { MassBalanceWaterfall } from '../components/common/MassBalanceWaterfall'
import { PageHeader } from '../components/common/PageHeader'
import { QualityFlagBadge } from '../components/common/QualityFlagBadge'
import { useAuth } from '../hooks/useAuth'
import { useBalanceRun } from '../hooks/useBalanceRun'
import { useTankStore } from '../stores/tankStore'
import type { BalanceRun, BalanceRunInput, BalanceStatus } from '../types/balance'
import { deviationLabels } from '../types/deviation'
import type { MeasurementSnapshot } from '../types/measurement'
import { dateTime, kg, localInputDate, number } from '../utils/format'

const statusLabels: Record<BalanceStatus, string> = {
  queued: '排队', calculating: '已计算', pending_review: '待复核',
  accepted: '已接受', rejected: '已驳回', invalidated: '已作废'
}

type BoundaryPreview = { opening: MeasurementSnapshot; closing: MeasurementSnapshot }

const byMeasuredAtDesc = (a: MeasurementSnapshot, b: MeasurementSnapshot) =>
  new Date(b.measured_at).getTime() - new Date(a.measured_at).getTime() || b.id - a.id

export function BalancesPage() {
  const { user, can } = useAuth()
  const store = useBalanceRun()
  const tanks = useTankStore()
  const [runOpen, setRunOpen] = useState(false)
  const [reviewTarget, setReviewTarget] = useState<'accepted' | 'rejected'>('accepted')
  const [reviewOpen, setReviewOpen] = useState(false)
  const [runForm] = Form.useForm<BalanceRunInput>()
  const [reviewForm] = Form.useForm<{ note: string }>()
  const [preview, setPreview] = useState<BoundaryPreview | null>(null)
  const [previewError, setPreviewError] = useState<string | null>(null)
  const [checking, setChecking] = useState(false)
  useEffect(() => { void Promise.all([store.load(), tanks.load()]) }, [])
  const selected = useMemo(() => store.items.find((item) => item.id === store.selectedId) ?? store.items[0], [store.items, store.selectedId])
  const openRun = () => {
    runForm.setFieldsValue({ tank_id: tanks.items[0]?.id })
    setPreview(null)
    setPreviewError(null)
    setRunOpen(true)
  }
  const checkBoundary = async () => {
    const values = runForm.getFieldsValue()
    if (!values.tank_id || !values.period_start || !values.period_end) {
      setPreview(null)
      setPreviewError('请先选择储罐和期间起止时间，再检查边界快照。')
      return
    }
    const startMs = new Date(values.period_start).getTime()
    const endMs = new Date(values.period_end).getTime()
    if (Number.isNaN(startMs) || Number.isNaN(endMs) || endMs <= startMs) {
      setPreview(null)
      setPreviewError('平衡期间结束时间必须晚于开始时间。')
      return
    }
    setChecking(true)
    setPreviewError(null)
    try {
      const result = await listMeasurements(values.tank_id)
      const usable = result.items.filter((item) => item.quality_flag !== 'invalid')
      const opening = usable
        .filter((item) => new Date(item.measured_at).getTime() <= startMs)
        .sort(byMeasuredAtDesc)[0]
      if (!opening) {
        setPreview(null)
        setPreviewError('期间开始前缺少 good 或 suspect 的期初快照，本次运行将被拒绝。')
        return
      }
      const openingMs = new Date(opening.measured_at).getTime()
      const closing = usable
        .filter((item) => {
          const measuredMs = new Date(item.measured_at).getTime()
          return measuredMs >= startMs && measuredMs <= endMs && measuredMs > openingMs
        })
        .sort(byMeasuredAtDesc)[0]
      if (!closing) {
        setPreview(null)
        setPreviewError('期间内缺少晚于期初快照的有效期末快照，本次运行将被拒绝。')
        return
      }
      setPreview({ opening, closing })
    } catch {
      setPreview(null)
      setPreviewError('边界快照查询失败，请稍后重试。')
    } finally {
      setChecking(false)
    }
  }
  const run = async (values: BalanceRunInput) => {
    if (!preview) {
      setPreviewError('请先检查边界快照质量，确认放行依据后再执行计算。')
      return
    }
    const openingBasis = values.opening_release_basis?.trim() ?? ''
    const closingBasis = values.closing_release_basis?.trim() ?? ''
    if (preview.opening.quality_flag === 'suspect' && openingBasis.length < 6) {
      setPreviewError('期初快照质量为可疑，必须填写不少于 6 个字符的放行依据。')
      return
    }
    if (preview.closing.quality_flag === 'suspect' && closingBasis.length < 6) {
      setPreviewError('期末快照质量为可疑，必须填写不少于 6 个字符的放行依据。')
      return
    }
    try {
      await store.run({
        tank_id: values.tank_id,
        period_start: new Date(values.period_start).toISOString(),
        period_end: new Date(values.period_end).toISOString(),
        opening_release_basis: preview.opening.quality_flag === 'suspect' ? openingBasis : undefined,
        closing_release_basis: preview.closing.quality_flag === 'suspect' ? closingBasis : undefined
      })
      setRunOpen(false)
    } catch {
      // 依据缺失等错误只拒绝本次运行：保留弹窗、表单和已填依据，不产生半成品记录。
    }
  }
  const review = async ({ note }: { note: string }) => {
    if (!selected) return
    await store.review(selected, reviewTarget, note)
    setReviewOpen(false)
    reviewForm.resetFields()
  }
  const openReview = (target: 'accepted' | 'rejected') => {
    setReviewTarget(target)
    setReviewOpen(true)
  }
  const releasedBoundaries = selected ? [
    { key: 'opening', label: '期初快照', id: selected.opening_snapshot_id, basis: selected.opening_release_basis },
    { key: 'closing', label: '期末快照', id: selected.closing_snapshot_id, basis: selected.closing_release_basis }
  ].filter((item) => item.basis) : []
  return (
    <>
      <PageHeader
        eyebrow="MASS BALANCE WORKBENCH"
        title="平衡工作台"
        description="重放期初、物理转移和期末证据，输出 BOG / 未解释项及其不确定度关系。"
        actions={
          <>
            <Button icon={<RefreshCw size={16} />} onClick={() => void store.load()}>刷新</Button>
            {can('process_analyst', 'admin') && <Button type="primary" icon={<Play size={16} />} onClick={openRun}>运行平衡</Button>}
          </>
        }
      />
      <section className="balance-layout">
        <div className="balance-main">
          <div className="chart-panel">
            <div className="section-heading">
              <div><h2>质量边界瀑布</h2><span>{selected ? selected.tank?.tank_code + ' · ' + dateTime(selected.period_end) : '尚未选择运行'}</span></div>
              {selected && <Tag color={selected.deviation_level === 'investigate' ? 'warning' : 'success'}>{deviationLabels[selected.deviation_level]}</Tag>}
            </div>
            <MassBalanceWaterfall run={selected} />
            {selected && (
              <div className="metric-strip">
                <div><span>BOG / 未解释项</span><strong>{kg(selected.estimated_bog_kg)}</strong></div>
                <div><span>不确定度</span><strong>± {kg(selected.uncertainty_kg)}</strong></div>
                <div><span>偏差率</span><strong>{number.format(selected.deviation_pct)}%</strong></div>
                <div><span>状态</span><strong>{statusLabels[selected.balance_status]}</strong></div>
              </div>
            )}
          </div>
          <EvidenceBreakdownPanel run={selected} />
        </div>
        <aside className="run-rail">
          <div className="section-heading"><h2>运行历史</h2><span>{store.items.length} 条</span></div>
          <Table<BalanceRun>
            rowKey="id"
            size="small"
            loading={store.loading}
            dataSource={store.items}
            pagination={{ pageSize: 8, showSizeChanger: false }}
            onRow={(item) => ({ onClick: () => store.select(item.id) })}
            rowClassName={(item) => item.id === selected?.id ? 'selected-row' : ''}
            columns={[
              { title: '运行', key: 'run', render: (_, item) => <><strong>#{item.id} · {item.tank?.tank_code ?? item.tank_id}</strong><div className="secondary">{dateTime(item.period_end)}</div></> },
              { title: '状态', dataIndex: 'balance_status', width: 92, render: (value: BalanceStatus) => <Tag>{statusLabels[value]}</Tag> }
            ]}
          />
          {selected && (
            <div className="workflow-actions">
              {selected.balance_status === 'calculating' && can('process_analyst', 'admin') && <Button type="primary" icon={<Send size={16} />} loading={store.working} onClick={() => void store.submit(selected)} block>提交独立复核</Button>}
              {selected.balance_status === 'pending_review' && can('reviewer', 'admin') && (
                <>
                  <Button type="primary" icon={<CheckCircle2 size={16} />} onClick={() => openReview('accepted')} block>接受结果</Button>
                  <Button danger icon={<XCircle size={16} />} onClick={() => openReview('rejected')} block>驳回结果</Button>
                </>
              )}
              {releasedBoundaries.length > 0 && (
                <Alert
                  type="warning"
                  showIcon
                  icon={<ShieldAlert size={16} />}
                  message="本运行含已放行的可疑边界快照"
                  description={
                    <ul className="release-basis-list">
                      {releasedBoundaries.map((item) => (
                        <li key={item.key}><strong>{item.label} #{item.id}</strong><span>{item.basis}</span></li>
                      ))}
                    </ul>
                  }
                />
              )}
              {selected.review_note && <Alert type="info" showIcon message={selected.review_note} />}
            </div>
          )}
        </aside>
      </section>
      <Modal title="运行物理质量平衡" open={runOpen} onCancel={() => setRunOpen(false)} footer={null} destroyOnClose width={640}>
        <Form<BalanceRunInput>
          form={runForm}
          layout="vertical"
          onFinish={run}
          requiredMark={false}
          initialValues={{
            period_start: localInputDate(new Date(Date.now() - 24 * 3_600_000)),
            period_end: localInputDate(new Date())
          }}
          onValuesChange={(changed) => {
            if ('tank_id' in changed || 'period_start' in changed || 'period_end' in changed) {
              setPreview(null)
              setPreviewError(null)
            }
          }}
        >
          <Form.Item name="tank_id" label="储罐" rules={[{ required: true }]}><Select options={tanks.items.map((tank) => ({ value: tank.id, label: tank.tank_code + ' · ' + tank.name }))} /></Form.Item>
          <div className="form-grid">
            <Form.Item name="period_start" label="期间开始" rules={[{ required: true }]}><Input type="datetime-local" /></Form.Item>
            <Form.Item name="period_end" label="期间结束" rules={[{ required: true }]}><Input type="datetime-local" /></Form.Item>
          </div>
          <div className="boundary-check-row">
            <Button icon={<Search size={16} />} loading={checking} onClick={() => void checkBoundary()} block>检查边界快照质量</Button>
          </div>
          {previewError && <Alert className="form-alert" type="error" showIcon message={previewError} />}
          {preview && (
            <div className="boundary-preview">
              <div className="boundary-preview-item">
                <div className="boundary-preview-head"><strong>期初快照 #{preview.opening.id}</strong><QualityFlagBadge value={preview.opening.quality_flag} /></div>
                <div className="secondary">{dateTime(preview.opening.measured_at)} · 液相质量 {kg(preview.opening.calculated_liquid_mass_kg)}</div>
              </div>
              <div className="boundary-preview-item">
                <div className="boundary-preview-head"><strong>期末快照 #{preview.closing.id}</strong><QualityFlagBadge value={preview.closing.quality_flag} /></div>
                <div className="secondary">{dateTime(preview.closing.measured_at)} · 液相质量 {kg(preview.closing.calculated_liquid_mass_kg)}</div>
              </div>
            </div>
          )}
          {preview?.opening.quality_flag === 'suspect' && (
            <Form.Item
              name="opening_release_basis"
              label="期初快照质量放行依据"
              rules={[{ required: true, min: 6, max: 1000, message: '可疑期初快照必须填写 6-1000 字的放行依据' }]}
            >
              <Input.TextArea rows={2} maxLength={1000} showCount placeholder="填写校准/复核记录、责任人或工艺判据；运行建立后依据将被固化。" />
            </Form.Item>
          )}
          {preview?.closing.quality_flag === 'suspect' && (
            <Form.Item
              name="closing_release_basis"
              label="期末快照质量放行依据"
              rules={[{ required: true, min: 6, max: 1000, message: '可疑期末快照必须填写 6-1000 字的放行依据' }]}
            >
              <Input.TextArea rows={2} maxLength={1000} showCount placeholder="填写校准/复核记录、责任人或工艺判据；运行建立后依据将被固化。" />
            </Form.Item>
          )}
          <Alert className="form-alert" type="warning" showIcon message="系统将固化当前两条边界快照、放行依据与罐容系数；新增快照不会改写旧结果，重算会另建运行记录。" />
          <Button type="primary" htmlType="submit" icon={<Play size={16} />} loading={store.working} disabled={!preview} block>执行计算</Button>
        </Form>
      </Modal>
      <Modal title={reviewTarget === 'accepted' ? '接受平衡结果' : '驳回平衡结果'} open={reviewOpen} onCancel={() => setReviewOpen(false)} footer={null} destroyOnClose>
        <Form form={reviewForm} layout="vertical" onFinish={review} requiredMark={false}>
          <Form.Item name="note" label="独立复核意见" rules={[{ required: true, min: 6, max: 1000 }]}><Input.TextArea rows={4} /></Form.Item>
          <Button type={reviewTarget === 'accepted' ? 'primary' : 'default'} danger={reviewTarget === 'rejected'} htmlType="submit" loading={store.working} block>
            确认{reviewTarget === 'accepted' ? '接受' : '驳回'}
          </Button>
        </Form>
      </Modal>
      {user?.role === 'reviewer' && !store.items.some((item) => item.balance_status === 'pending_review') && (
        <Alert className="bottom-alert" type="info" showIcon message="当前没有待独立复核的平衡运行。" />
      )}
    </>
  )
}

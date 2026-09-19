import { Alert, Descriptions, Table, Tag } from 'antd'
import { QualityFlagBadge } from './QualityFlagBadge'
import type { BalanceRun, BoundaryRelease, UncertaintyComponent } from '../../types/balance'
import { deviationLabels } from '../../types/deviation'
import { dateTime, kg, number } from '../../utils/format'

const sourceLabels: Record<string, string> = {
  opening_snapshot: '期初快照',
  closing_snapshot: '期末快照',
  transfer_inflow: '流入计量',
  transfer_outflow: '流出计量'
}

const deviationHints: Record<string, string> = {
  within_uncertainty: '实际偏差落在合成不确定度区间内。',
  watch: '实际偏差超过合成不确定度但未超过 2 倍区间，需要关注。',
  investigate: '实际偏差超过 2 倍合成不确定度，需独立调查但不直接认定为泄漏。',
  invalid: '边界快照无效，偏差关系不可用。'
}

function BoundaryReleaseTable({ releases }: { releases: BoundaryRelease[] }) {
  return (
    <div className="boundary-release-panel">
      <div className="section-heading">
        <div>
          <span className="eyebrow">BOUNDARY SNAPSHOT RELEASE</span>
          <h3>边界快照质量放行</h3>
        </div>
      </div>
      <Table<BoundaryRelease>
        className="evidence-table"
        rowKey={(item) => item.snapshot_id + item.measured_at}
        size="small"
        pagination={false}
        dataSource={releases}
        columns={[
          { title: '边界', key: 'boundary', width: 90, render: (_, item) => item === releases[0] ? '期初' : '期末' },
          { title: '快照', key: 'snapshot', render: (_, item) => <>#{item.snapshot_id}<div className="secondary">{dateTime(item.measured_at)}</div></> },
          { title: '质量', dataIndex: 'quality_flag', width: 92, render: (value: BoundaryRelease['quality_flag']) => <QualityFlagBadge value={value} /> },
          { title: '放行', dataIndex: 'released', width: 80, align: 'center', render: (released: boolean) => released ? <Tag color="warning">已放行</Tag> : <Tag color="success">无需放行</Tag> },
          { title: '放行依据', key: 'basis', render: (_, item) => item.released ? item.release_basis : <span className="secondary">good 快照沿用原质量平衡流程</span> }
        ]}
      />
    </div>
  )
}

export function EvidenceBreakdownPanel({ run }: { run?: BalanceRun }) {
  if (!run) return null
  const evidence = run.evidence_json ?? {}
  const uncertainty = evidence.uncertainty
  const releases = evidence.boundary_releases
    ? [evidence.boundary_releases.opening, evidence.boundary_releases.closing]
    : []
  return (
    <section className="evidence-panel" aria-label="平衡证据明细">
      <div className="section-heading">
        <div>
          <span className="eyebrow">EVIDENCE SNAPSHOT</span>
          <h2>证据与不确定度</h2>
        </div>
        <Tag color={run.deviation_level === 'investigate' ? 'warning' : 'success'}>
          {deviationLabels[run.deviation_level]}
        </Tag>
      </div>
      <Descriptions size="small" column={{ xs: 1, sm: 2, lg: 4 }} bordered>
        <Descriptions.Item label="算法版本">{evidence.algorithm_version ?? 'mass-balance-v1.0'}</Descriptions.Item>
        <Descriptions.Item label="系数版本">{run.coefficient_version}</Descriptions.Item>
        <Descriptions.Item label="合成不确定度">{kg(run.uncertainty_kg)}</Descriptions.Item>
        <Descriptions.Item label="偏差">{number.format(run.deviation_pct)}%</Descriptions.Item>
      </Descriptions>
      <Alert
        className="deviation-relation-alert"
        type={run.deviation_level === 'within_uncertainty' ? 'success' : 'warning'}
        showIcon
        message={`偏差关系：${deviationLabels[run.deviation_level]} · 区间 [${kg(run.interval_lower_kg)}, ${kg(run.interval_upper_kg)}]`}
        description={deviationHints[run.deviation_level]}
      />
      {releases.length > 0 && <BoundaryReleaseTable releases={releases} />}
      {uncertainty?.components?.length ? (
        <Table<UncertaintyComponent>
          className="evidence-table"
          rowKey={(item) => item.source + item.entity_id}
          size="small"
          pagination={false}
          dataSource={uncertainty.components}
          columns={[
            { title: '来源', dataIndex: 'source', render: (value: string) => sourceLabels[value] ?? value },
            { title: '证据 ID', dataIndex: 'entity_id' },
            { title: '质量', dataIndex: 'mass_kg', align: 'right', render: kg },
            { title: '不确定度', dataIndex: 'uncertainty_pct', align: 'right', render: (value: number) => number.format(value) + '%' },
            { title: '绝对贡献', dataIndex: 'absolute_kg', align: 'right', render: kg }
          ]}
        />
      ) : null}
      <Alert type="warning" showIcon message={evidence.safety_boundary ?? '未解释差异仅作工程分析，不直接认定为泄漏或安全事件。'} />
    </section>
  )
}

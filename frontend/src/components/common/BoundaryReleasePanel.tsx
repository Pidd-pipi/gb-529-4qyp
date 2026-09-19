import { Alert, Descriptions, Tag } from 'antd'
import type { BalanceRun, BoundaryReleaseEvidence } from '../../types/balance'
import { deviationLabels } from '../../types/deviation'
import { dateTime, kg } from '../../utils/format'
import { QualityFlagBadge } from './QualityFlagBadge'

function fallbackEvidence(run: BalanceRun, boundary: 'opening' | 'closing'): BoundaryReleaseEvidence | undefined {
  const snapshotId = boundary === 'opening' ? run.opening_snapshot_id : run.closing_snapshot_id
  const qualityFlag = boundary === 'opening' ? run.opening_quality_flag : run.closing_quality_flag
  const releaseNote = boundary === 'opening' ? run.opening_release_note : run.closing_release_note
  if (!snapshotId || !qualityFlag) return undefined
  return { snapshot_id: snapshotId, measured_at: '', quality_flag: qualityFlag, released: Boolean(releaseNote), release_note: releaseNote || undefined }
}

export function BoundaryReleasePanel({ run }: { run?: BalanceRun }) {
  if (!run) return null
  const stored = run.evidence_json?.boundary_release
  const opening = stored?.opening ?? fallbackEvidence(run, 'opening')
  const closing = stored?.closing ?? fallbackEvidence(run, 'closing')
  if (!opening && !closing) return null
  const releasedCount = [opening, closing].filter((item) => item?.released).length
  const renderBoundary = (label: string, item?: BoundaryReleaseEvidence) => {
    if (!item) return <span className="secondary">无记录</span>
    return (
      <span className="boundary-cell">
        <span>#{item.snapshot_id}{item.measured_at ? ' · ' + dateTime(item.measured_at) : ''}</span>
        <QualityFlagBadge value={item.quality_flag} />
      </span>
    )
  }
  return (
    <section className="evidence-panel" aria-label="边界快照放行">
      <div className="section-heading">
        <div>
          <span className="eyebrow">BOUNDARY RELEASE</span>
          <h2>边界快照放行</h2>
        </div>
        <Tag color={releasedCount > 0 ? 'warning' : 'success'}>{releasedCount > 0 ? `含 ${releasedCount} 条 suspect 放行` : '边界快照均为良好'}</Tag>
      </div>
      <Descriptions size="small" column={{ xs: 1, sm: 2 }} bordered>
        <Descriptions.Item label="期初快照">{renderBoundary('期初', opening)}</Descriptions.Item>
        <Descriptions.Item label="期末快照">{renderBoundary('期末', closing)}</Descriptions.Item>
        {opening?.released && (
          <Descriptions.Item label="期初放行依据" span={2}>{opening.release_note}</Descriptions.Item>
        )}
        {closing?.released && (
          <Descriptions.Item label="期末放行依据" span={2}>{closing.release_note}</Descriptions.Item>
        )}
        <Descriptions.Item label="偏差关系" span={2}>
          偏差 {kg(run.estimated_bog_kg)} 相对合成不确定度 ± {kg(run.uncertainty_kg)}：
          <Tag color={run.deviation_level === 'investigate' ? 'warning' : 'success'}>{deviationLabels[run.deviation_level]}</Tag>
        </Descriptions.Item>
      </Descriptions>
      {releasedCount > 0 && (
        <Alert type="info" showIcon message="放行依据已随本次运行固化，后续新增快照不会改写该结果；如需重算请另建运行。" />
      )}
    </section>
  )
}

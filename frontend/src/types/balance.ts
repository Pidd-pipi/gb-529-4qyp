import type { DeviationLevel } from './deviation'
import type { MeasurementSnapshot, QualityFlag } from './measurement'
import type { StorageTank } from './tank'

export type BalanceStatus = 'queued' | 'calculating' | 'pending_review' | 'accepted' | 'rejected' | 'invalidated'

export interface BalanceRun {
  id: number
  tank_id: number
  period_start: string
  period_end: string
  balance_status: BalanceStatus
  input_snapshot_json: Record<string, unknown>
  opening_snapshot_id: number
  closing_snapshot_id: number
  opening_quality_flag: QualityFlag
  closing_quality_flag: QualityFlag
  opening_release_note: string
  closing_release_note: string
  opening_mass_kg: number
  closing_mass_kg: number
  net_transfer_kg: number
  estimated_bog_kg: number
  uncertainty_kg: number
  interval_lower_kg: number
  interval_upper_kg: number
  deviation_pct: number
  deviation_level: DeviationLevel
  evidence_json: BalanceEvidence
  coefficient_version: string
  version: number
  created_by: number
  reviewed_by?: number
  review_note: string
  reviewed_at?: string
  created_at: string
  updated_at: string
  tank?: StorageTank
}

export interface UncertaintyComponent {
  source: string
  entity_id: number
  mass_kg: number
  uncertainty_pct: number
  absolute_kg: number
}

export interface UncertaintyBreakdown {
  balance_run_id?: number
  combined_kg: number
  lower_kg: number
  upper_kg: number
  relationship: DeviationLevel
  components: UncertaintyComponent[]
}

export interface BoundaryReleaseEvidence {
  snapshot_id: number
  measured_at: string
  quality_flag: QualityFlag
  released: boolean
  release_note?: string
}

export interface BalanceEvidence {
  algorithm_version?: string
  equation?: Record<string, number>
  uncertainty?: UncertaintyBreakdown
  boundary_release?: { opening?: BoundaryReleaseEvidence; closing?: BoundaryReleaseEvidence }
  safety_boundary?: string
}

export interface BalanceRunInput {
  tank_id: number
  period_start: string
  period_end: string
  opening_release_note?: string
  closing_release_note?: string
}

export interface BoundaryPreview {
  opening: MeasurementSnapshot
  closing: MeasurementSnapshot
  opening_release_required: boolean
  closing_release_required: boolean
}

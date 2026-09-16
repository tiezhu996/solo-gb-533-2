import { GeoJSONValue } from './robot-cell';
import { ZoneType } from './enums/zone-type';
import { RevisionStatus } from './enums/revision-status';

export interface ZoneRevisionImpact {
  motion_program_id: number;
  program_code: string;
  program_version: number;
  program_state: string;
  affected: boolean;
  collision_count: number;
  first_segment: number;
  first_time_ms: number;
  min_clearance_mm: number;
  actual_speed_mm_s: number;
  allowed_speed_mm_s: number;
  evidence: string;
}

export interface ReevaluationFlag {
  validation_run_id: number;
  motion_program_id: number;
  prior_status: string;
  flag_reason: string;
  created_at: string;
}

export interface ZoneRevision {
  id: number;
  safety_zone_id: number;
  robot_cell_id: number;
  robot_cell_code: string;
  zone_name: string;
  revision_status: RevisionStatus;
  base_version: number;
  published_version: number | null;
  name: string;
  zone_type: ZoneType;
  polygon_geojson: GeoJSONValue;
  min_height_mm: number;
  max_height_mm: number;
  speed_limit_mm_s: number;
  access_rule: string;
  created_at: string;
  updated_at: string;
  published_at: string | null;
  impacts: ZoneRevisionImpact[];
  reevaluation_flags: ReevaluationFlag[];
}

export interface SaveZoneRevisionDraft {
  name: string;
  zone_type: ZoneType;
  polygon_geojson: GeoJSONValue;
  min_height_mm: number;
  max_height_mm: number;
  speed_limit_mm_s: number;
  access_rule: string;
  expected_version?: number;
}

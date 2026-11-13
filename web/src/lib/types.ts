export type CaseStatus = "intake" | "active" | "review" | "closed" | "archived"
export type Priority = "low" | "medium" | "high" | "critical"
export type IntegrityStatus = "unverified" | "verified" | "failed"

export interface Case {
  id: string
  reference: string
  title: string
  description: string
  status: CaseStatus
  priority: Priority
  tags: string[]
  created_at: string
  updated_at: string
  evidence_count: number
  total_bytes: number
}

export interface Artifact {
  id: string
  evidence_id: string
  role: string
  filename: string
  mime_type: string
  size_bytes: number
  sha256: string
  sha1: string
  md5: string
  client_sha256: string | null
  created_at: string
}

export interface Evidence {
  id: string
  case_id: string
  case_reference: string
  case_title: string
  number: number
  exhibit_ref: string
  kind: "file" | "web_capture"
  title: string
  description: string
  source: string
  acquisition_method: string
  acquired_at: string
  location: string
  tags: string[]
  integrity_status: IntegrityStatus
  last_verified_at: string | null
  created_at: string
  total_bytes: number
  primary_sha256: string
  primary_mime: string
  primary_artifact_id: string | null
  artifact_count: number
  artifacts?: Artifact[]
  capture?: CaptureSummary | null
}

export interface CustodyEvent {
  seq: number
  id: string
  case_id: string | null
  evidence_id: string | null
  action: string
  actor: string
  details: Record<string, unknown>
  occurred_at: string
  prev_hash: string
  hash: string
  case_reference: string | null
  exhibit_ref: string | null
  evidence_title: string | null
}

export interface Note {
  id: string
  body: string
  created_at: string
}

export interface ChainResult {
  valid: boolean
  checked: number
  broken_at?: number
  reason?: string
  head_seq: number
  head_hash: string
  verified_at: string
}

export interface Dashboard {
  cases_by_status: Record<CaseStatus, number>
  total_cases: number
  total_evidence: number
  total_bytes: number
  integrity: Record<IntegrityStatus, number>
  recent_activity: CustodyEvent[]
  chain_head: { seq: number; hash: string }
  open_cases: Case[]
}

export interface Settings {
  investigator_name: string
  organization: string
  case_prefix: string
  version: string
}

export interface SearchResults {
  cases: Case[]
  evidence: Evidence[]
}

export interface Paged<T> {
  items: T[]
  total: number
  limit: number
  offset: number
}

export interface CasePage extends Paged<Case> {
  counts: Record<CaseStatus, number>
  all: number
}

export interface DnsRecord {
  name: string
  type: string
  ttl: number
  value: string
}

export interface DnsQuery {
  name: string
  type: string
  rcode: string
  records: DnsRecord[]
  error?: string
}

export interface CaptureSummary {
  requested_url: string
  final_url: string
  title: string
  started_at: string
  completed_at: string
  browser: string
  user_agent: string
  viewport: string
  redirects: { url: string; status: number; location: string }[]
  http: {
    status: number
    status_text: string
    protocol: string
    remote_ip: string
    remote_port: number
    mime_type: string
    headers: Record<string, string>
  } | null
  tls: {
    protocol: string
    cipher: string
    key_exchange: string
    subject_name: string
    issuer: string
    sans: string[]
    valid_from: string
    valid_to: string
  } | null
  dns: {
    resolver: string
    queried_at: string
    hosts: string[]
    registered_domains: string[]
    queries: DnsQuery[]
  } | null
}

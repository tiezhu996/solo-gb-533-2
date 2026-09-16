import { ChangeDetectionStrategy, Component, computed, input, output, OnChanges, SimpleChanges, inject } from '@angular/core';
import { DecimalPipe } from '@angular/common';
import { MatButtonModule } from '@angular/material/button';
import { LucideAngularModule } from 'lucide-angular';
import { SafetyZone } from '../../types/safety-zone';
import { ZoneRevisionStore } from '../../stores/zone-revision.store';
import { useAuth } from '../../hooks/use-auth';

@Component({
  selector: 'app-revision-panel',
  standalone: true,
  imports: [DecimalPipe, MatButtonModule, LucideAngularModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <section class="revision-panel">
      <header>
        <div><span>Revision impact</span><h2>{{ zone().name }} · v{{ zone().version }}</h2></div>
        <div class="head-actions">
          @if (canEdit()) {
            <button mat-stroked-button type="button" (click)="revise.emit(zone())"><lucide-icon name="git-branch" [size]="15" />{{ revisions.draft() ? 'Edit open draft on canvas' : 'Revise on canvas' }}</button>
            @if (revisions.draft(); as draft) {
              <button mat-flat-button color="primary" type="button" [disabled]="revisions.publishing()" (click)="publish()">
                <lucide-icon name="circle-check" [size]="15" />{{ revisions.publishing() ? 'Publishing…' : 'Publish draft as v' + (zone().version + 1) }}
              </button>
            }
          }
          <button mat-icon-button type="button" aria-label="Refresh revision data" (click)="reload()"><lucide-icon name="refresh-cw" [size]="16" /></button>
        </div>
      </header>

      @if (revisions.error()) { <p class="error-banner"><lucide-icon name="triangle-alert" [size]="15" />{{ revisions.error() }}</p> }
      @if (revisions.notice()) { <p class="notice"><lucide-icon name="circle-check" [size]="14" />{{ revisions.notice() }}</p> }

      @if (revisions.draft(); as draft) {
        <div class="draft-bar">
          <lucide-icon name="file-code-2" [size]="15" />
          <span>One unpublished draft open · base v{{ draft.base_version }} → target v{{ zone().version + 1 }} · {{ draft.zone_type }}</span>
          <strong>{{ draft.impacts.length ? draft.impacts.length + ' programs evaluated' : 'impact list fixed on publish' }}</strong>
        </div>
      } @else {
        <p class="empty-draft">No open draft. A zone may hold at most one unpublished draft; publishing assigns a unique new version and freezes the impact list.</p>
      }

      <div class="impact-grid">
        <div class="impact-col">
          <h3>Active programs in cell after latest publish</h3>
          @if (published(); as published) {
            <table>
              <thead><tr><th>Program</th><th>Affected</th><th>Contacts</th><th>First contact</th><th>Clearance</th><th>Speed (actual/limit)</th></tr></thead>
              <tbody>
                @for (impact of published.impacts; track impact.motion_program_id) {
                  <tr [class.affected]="impact.affected">
                    <td><strong>{{ impact.program_code }}</strong><small>v{{ impact.program_version }} · {{ impact.program_state }}</small></td>
                    <td><span class="tag" [class.hit]="impact.affected">{{ impact.affected ? 'affected' : 'clear' }}</span></td>
                    <td>{{ impact.collision_count }}</td>
                    <td>@if (impact.first_time_ms >= 0) { {{ impact.first_time_ms | number:'1.0-0' }} ms <small>seg {{ impact.first_segment }}</small> } @else { — }</td>
                    <td>@if (impact.first_time_ms >= 0) { {{ impact.min_clearance_mm | number:'1.0-1' }} mm } @else { — }</td>
                    <td>@if (impact.first_time_ms >= 0) { {{ impact.actual_speed_mm_s | number:'1.0-0' }} / {{ impact.allowed_speed_mm_s | number:'1.0-0' }} } @else { — }</td>
                  </tr>
                } @empty { <tr><td colspan="6">No active programs were evaluated.</td></tr> }
              </tbody>
            </table>
            @if (published.impacts.length) {
              <ul class="evidence">
                @for (impact of published.impacts; track impact.motion_program_id) { <li><b>{{ impact.program_code }}:</b> {{ impact.evidence }}</li> }
              </ul>
            }
          } @else { <p class="muted">No published revision yet. The impact list is generated together with the new version on publish.</p> }
        </div>
        <div class="flag-col">
          <h3>Accepted validations awaiting re-evaluation</h3>
          @if (published(); as published) {
            @for (flag of published.reevaluation_flags; track flag.validation_run_id) {
              <div class="flag">
                <lucide-icon name="clipboard-check" [size]="15" />
                <div><strong>Validation #{{ flag.validation_run_id }}</strong><small>program #{{ flag.motion_program_id }} · was {{ flag.prior_status }}</small><p>{{ flag.flag_reason }}</p></div>
              </div>
            } @empty { <p class="muted">No accepted validation needs re-evaluation.</p> }
          } @else { <p class="muted">Flags appear only for previously accepted validations of affected active programs.</p> }
          <p class="preservation"><lucide-icon name="shield-check" [size]="13" /> Accepted validations are only flagged. Their status, findings and risk evidence are never modified and stay readable.</p>
        </div>
      </div>
    </section>
  `,
  styles: [`
    .revision-panel{margin-top:16px;background:#fafbf8;border:1px solid #bec8c4;border-radius:4px;overflow:hidden}.revision-panel>header{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:13px 15px;background:#e6ebe8;border-bottom:1px solid #c6cfcc;flex-wrap:wrap}.revision-panel header span{font-size:10px;text-transform:uppercase;color:#6a7679}.revision-panel h2{margin:3px 0 0;font-size:15px}.head-actions{display:flex;align-items:center;gap:8px}.head-actions button{display:flex;gap:6px;align-items:center}.notice{display:flex;align-items:center;gap:7px;margin:0;padding:9px 15px;background:#e9f1ea;color:#275c3a;font-size:11px;border-bottom:1px solid #c9ddd0}.error-banner{display:flex;align-items:center;gap:7px;margin:0;padding:9px 15px;background:#f7e7e5;color:#8e2f28;font-size:11px;border-bottom:1px solid #e3c2bf}.draft-bar{display:flex;align-items:center;gap:9px;padding:10px 15px;background:#f7efcf;border-bottom:1px solid #e2d49a;color:#5f4d0c;font-size:11px}.draft-bar strong{margin-left:auto;text-transform:uppercase;font-size:9px;color:#7a6410}.empty-draft{margin:0;padding:11px 15px;color:#667376;font-size:11px;border-bottom:1px solid #e2e6e4}.impact-grid{display:grid;grid-template-columns:minmax(0,1.5fr) minmax(260px,.8fr);gap:0}.impact-col{padding:13px 15px;border-right:1px solid #e0e5e3}.flag-col{padding:13px 15px}.impact-grid h3{margin:0 0 10px;font-size:10px;text-transform:uppercase;color:#6a7679}table{width:100%;border-collapse:collapse;font-size:11px}th{text-align:left;font-size:9px;text-transform:uppercase;color:#768184;padding:5px 7px;border-bottom:1px solid #d4dbd8}td{padding:7px;border-bottom:1px solid #e8ecea;vertical-align:top}td strong{display:block;font-size:11px}td small{color:#7c888b;font-size:9px;text-transform:uppercase}tr.affected{background:#faf3f2}.tag{font-size:9px;text-transform:uppercase;padding:2px 7px;border-radius:10px;background:#e7ecea;color:#5a676a}.tag.hit{background:#f0d5d2;color:#8e2f28}.evidence{margin:10px 0 0;padding-left:16px;font-size:10px;color:#5b676a}.evidence li{margin-bottom:4px}.muted{color:#7c888b;font-size:11px}.flag{display:flex;gap:9px;padding:10px;background:#fdf6e7;border:1px solid #e7d5a2;border-radius:4px;margin-bottom:9px;color:#6b5410}.flag strong{display:block;font-size:11px;color:#53430d}.flag small{display:block;font-size:9px;text-transform:uppercase;color:#8a7739}.flag p{margin:4px 0 0;font-size:10px}.preservation{display:flex;gap:6px;align-items:flex-start;margin-top:12px;color:#5b676a;font-size:10px}
    @media(max-width:1020px){.impact-grid{grid-template-columns:1fr}.impact-col{border-right:0;border-bottom:1px solid #e0e5e3}.impact-col table{display:block;overflow-x:auto}}
  `],
})
export class RevisionPanelComponent implements OnChanges {
  readonly zone = input.required<SafetyZone>();
  readonly revise = output<SafetyZone>();
  readonly publishedChange = output<void>();
  readonly revisions = inject(ZoneRevisionStore);
  private readonly auth = useAuth();
  readonly published = computed(() => this.revisions.lastPublished());
  canEdit = (): boolean => this.auth.can('safety_engineer', 'admin');

  ngOnChanges(changes: SimpleChanges): void {
    if (changes['zone']) this.reload();
  }
  reload(): void { this.revisions.loadAll(this.zone().id); }
  publish(): void {
    this.revisions.publish(this.zone().id, this.zone().version, () => {
      this.reload();
      this.publishedChange.emit();
    });
  }
}

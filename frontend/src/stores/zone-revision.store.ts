import { inject, Injectable, signal } from '@angular/core';
import { HttpErrorResponse } from '@angular/common/http';
import { finalize } from 'rxjs';
import { ZoneRevisionApi } from '../api/zone-revision';
import { SaveZoneRevisionDraft, ZoneRevision } from '../types/zone-revision';
import { apiErrorMessage } from '../utils/api-error';

@Injectable({ providedIn: 'root' })
export class ZoneRevisionStore {
  private readonly api = inject(ZoneRevisionApi);
  readonly draft = signal<ZoneRevision | null>(null);
  readonly history = signal<ZoneRevision[]>([]);
  readonly lastPublished = signal<ZoneRevision | null>(null);
  readonly loading = signal(false);
  readonly publishing = signal(false);
  readonly error = signal('');
  readonly notice = signal('');

  loadAll(zoneId: number): void {
    this.loading.set(true);
    this.error.set('');
    this.notice.set('');
    this.api.list(zoneId).pipe(finalize(() => this.loading.set(false))).subscribe({
      next: ({ data }) => {
        this.history.set(data);
        const open = data.find((revision) => revision.revision_status === 'draft') ?? null;
        this.draft.set(open);
        this.lastPublished.set(data.find((revision) => revision.revision_status === 'published') ?? null);
      },
      error: (error) => this.error.set(apiErrorMessage(error)),
    });
  }

  saveDraft(zoneId: number, payload: SaveZoneRevisionDraft, done?: () => void): void {
    this.loading.set(true);
    this.error.set('');
    this.api.saveDraft(zoneId, payload).pipe(finalize(() => this.loading.set(false))).subscribe({
      next: ({ data }) => {
        this.draft.set(data);
        this.notice.set('Unpublished draft saved. Only one open draft is kept for this zone.');
        done?.();
      },
      error: (error) => this.error.set(apiErrorMessage(error)),
    });
  }

  publish(zoneId: number, expectedVersion: number, done?: (published: ZoneRevision) => void): void {
    this.publishing.set(true);
    this.error.set('');
    this.api.publish(zoneId, expectedVersion).pipe(finalize(() => this.publishing.set(false))).subscribe({
      next: ({ data }) => {
        this.draft.set(null);
        this.lastPublished.set(data);
        this.history.update((items) => [data, ...items.filter((item) => item.id !== data.id)]);
        this.notice.set(`Revision published as v${data.published_version ?? ''}. Version and impact list were fixed together; accepted validations were only flagged for re-evaluation.`);
        done?.(data);
      },
      error: (error) => this.error.set(apiErrorMessage(error)),
    });
  }

  static isMissingDraft(error: unknown): boolean {
    return error instanceof HttpErrorResponse && error.error?.error?.code === 'draft_missing';
  }

  clear(): void {
    this.draft.set(null);
    this.history.set([]);
    this.lastPublished.set(null);
    this.error.set('');
    this.notice.set('');
  }
}

const TOAST_MAX_VISIBLE = 3;
const TOAST_MAX_QUEUED = 20;
const TOAST_DEDUPE_WINDOW_MS = 15000;
const TOAST_PREVIEW_MAX_LINES = 4;
const TOAST_PREVIEW_MAX_CHARS = 240;
const TOAST_KEY_MAX_CHARS = 180;
const TOAST_KEY_PREFIX_CHARS = 120;
const DEFAULT_TOAST_DURATION = 9000;
const MIN_TOAST_DURATION = 5000;

function normalizeType(type) {
  return ['info', 'success', 'warning', 'error'].includes(type) ? type : 'info';
}

function normalizeDetails(details) {
  if (!details) return [];
  const values = Array.isArray(details) ? details : [details];
  return values
    .flatMap((value) => String(value ?? '').split(/\r?\n/))
    .map((line) => line.trimEnd())
    .filter((line) => line.trim());
}

function compactText(parts) {
  return parts
    .map((part) => String(part ?? '').replace(/\r\n/g, '\n').trimEnd())
    .filter((part) => part.trim())
    .join('\n');
}

function previewText(text) {
  const full = String(text ?? '').replace(/\r\n/g, '\n').trim();
  if (!full) return { text: '', truncated: false };

  const lines = full.split('\n');
  let preview = lines.slice(0, TOAST_PREVIEW_MAX_LINES).join('\n');
  let truncated = lines.length > TOAST_PREVIEW_MAX_LINES;

  if (preview.length > TOAST_PREVIEW_MAX_CHARS) {
    preview = preview.slice(0, TOAST_PREVIEW_MAX_CHARS);
    truncated = true;
  }

  preview = preview.replace(/\s+$/g, '');
  if (truncated && !preview.endsWith('...')) preview += '...';
  return { text: preview, truncated };
}

function hashString(hash, value) {
  const text = String(value ?? '');
  for (let i = 0; i < text.length; i++) {
    hash ^= text.charCodeAt(i);
    hash = Math.imul(hash, 16777619) >>> 0;
  }
  return hash;
}

function hashPart(hash, part, seen = new WeakSet()) {
  if (part === null || part === undefined) return hashString(hash, '');
  if (Array.isArray(part)) {
    hash = hashString(hash, `[${part.length}:`);
    for (const item of part) hash = hashPart(hashString(hash, '|'), item, seen);
    return hashString(hash, ']');
  }
  if (typeof part === 'object') {
    if (seen.has(part)) return hashString(hash, '[circular]');
    seen.add(part);
    const keys = Object.keys(part).sort();
    hash = hashString(hash, `{${keys.length}:`);
    for (const key of keys) {
      hash = hashString(hash, key);
      hash = hashPart(hashString(hash, '='), part[key], seen);
    }
    seen.delete(part);
    return hashString(hash, '}');
  }
  return hashString(hash, part);
}

function keyPreview(part) {
  if (part === null || part === undefined) return '';
  if (Array.isArray(part)) return `list:${part.length}`;
  if (typeof part === 'object') return `object:${Object.keys(part).length}`;
  return String(part).trim().replace(/\s+/g, ' ').slice(0, 48);
}

function normalizeToastKey(parts) {
  let hash = 2166136261;
  let prefix = '';
  for (const part of parts) {
    hash = hashPart(hashString(hash, '\u001f'), part);
    const preview = keyPreview(part);
    if (!preview) continue;
    const next = prefix ? `${prefix}:${preview}` : preview;
    prefix = next.length > TOAST_KEY_PREFIX_CHARS ? next.slice(0, TOAST_KEY_PREFIX_CHARS) : next;
  }

  const suffix = `#${hash.toString(36).padStart(7, '0')}`;
  const base = prefix || 'toast';
  return `${base.slice(0, TOAST_KEY_MAX_CHARS - suffix.length)}${suffix}`;
}

export default {
  state: {
    toasts: [],
    toastQueue: [],
    toastOverflowCount: 0,
    _toastSequence: 0,
  },

  methods: {
    showToast(input, type = 'info', duration = DEFAULT_TOAST_DURATION, options = {}) {
      const toast = this._normalizeToast(input, type, duration, options);
      const duplicate = this._findDuplicateToast(toast.key, Date.now());
      if (duplicate) {
        this._mergeDuplicateToast(duplicate, toast);
        return duplicate.id;
      }

      if (this.toasts.length < TOAST_MAX_VISIBLE) {
        this.toasts = [...this.toasts, toast];
        this._startToastTimer(toast, true);
      } else if (this.toastQueue.length < TOAST_MAX_QUEUED) {
        this.toastQueue = [...this.toastQueue, toast];
      } else {
        this.toastOverflowCount += 1;
      }

      return toast.id;
    },

    toastKey(...parts) {
      return normalizeToastKey(parts);
    },

    dismissToast(id) {
      const active = this.toasts.find((toast) => toast.id === id);
      if (active) this._clearToastTimer(active);

      const beforeVisible = this.toasts.length;
      this.toasts = this.toasts.filter((toast) => toast.id !== id);
      this.toastQueue = this.toastQueue.filter((toast) => toast.id !== id);

      if (beforeVisible !== this.toasts.length) {
        this._promoteQueuedToasts();
      }
    },

    pauseToast(id) {
      const toast = this.toasts.find((item) => item.id === id);
      if (!toast || toast.paused || !toast.duration) return;
      if (toast.timerId) {
        const elapsed = Date.now() - toast.startedAt;
        toast.remainingMs = Math.max(0, toast.remainingMs - elapsed);
        this._clearToastTimer(toast);
      }
      toast.paused = true;
      this._syncToastCollections();
    },

    resumeToast(id) {
      const toast = this.toasts.find((item) => item.id === id);
      if (!toast || toast.expanded || !toast.paused || !toast.duration) return;
      toast.paused = false;
      if (toast.remainingMs <= 0) {
        this.dismissToast(id);
        return;
      }
      this._startToastTimer(toast, false);
    },

    toggleToast(id) {
      const toast = this.toasts.find((item) => item.id === id);
      if (!toast || !toast.expandable) return;

      toast.expanded = !toast.expanded;
      if (toast.expanded) {
        this.pauseToast(id);
      } else {
        this.resumeToast(id);
      }
      this._syncToastCollections();
    },

    toastDisplayText(toast) {
      if (!toast) return '';
      return toast.expanded ? toast.fullText : toast.previewText;
    },

    toastClass(toast) {
      const type = normalizeType(toast?.type);
      const compactBody = `${toast?.title || ''} ${toast?.fullText || ''}`.trim();
      const compact = !toast?.expandable && compactBody.length <= 110;
      return [
        'toast',
        `toast-${type}`,
        compact ? 'toast-compact' : '',
        toast?.expanded ? 'toast-expanded' : '',
      ].filter(Boolean).join(' ');
    },

    toastRole(toast) {
      return toast?.type === 'error' || toast?.type === 'warning' ? 'alert' : 'status';
    },

    toastProgressStyle(toast) {
      if (!toast?.duration) return 'display:none';
      const remaining = Math.max(0, toast.remainingMs || toast.duration);
      const state = toast.paused || toast.expanded ? 'paused' : 'running';
      return `animation-duration:${remaining}ms;animation-play-state:${state}`;
    },

    toastPendingCount() {
      return this.toastQueue.length + this.toastOverflowCount;
    },

    toastPendingLabel() {
      const queued = this.toastQueue.length;
      const collapsed = this.toastOverflowCount;
      const queuedLabel = queued === 1 ? '1 queued' : `${queued} queued`;
      const collapsedLabel = collapsed === 1 ? '1 collapsed' : `${collapsed} collapsed`;
      if (queued && collapsed) return `${queuedLabel}, ${collapsedLabel}`;
      if (queued) return queuedLabel;
      if (collapsed) return collapsedLabel;
      return '';
    },

    _normalizeToast(input, type, duration, options = {}) {
      const structured = input && typeof input === 'object' && !Array.isArray(input);
      const raw = structured ? input : {};
      const toastType = normalizeType(raw.type || type);
      const title = String(raw.title ?? options.title ?? '').trim();
      const details = normalizeDetails(raw.details ?? options.details);
      const message = String(structured ? (raw.message ?? raw.text ?? '') : (input ?? '')).trim();
      const fullText = compactText([message, ...details]);
      const preview = previewText(fullText || title);
      const hasExplicitDuration = structured ? raw.duration !== undefined : duration !== undefined;
      const parsedDuration = Number(hasExplicitDuration ? (raw.duration ?? duration) : DEFAULT_TOAST_DURATION);
      const rawDuration = Number.isFinite(parsedDuration) ? parsedDuration : DEFAULT_TOAST_DURATION;
      const toastDuration = rawDuration > 0 && !hasExplicitDuration
        ? Math.max(MIN_TOAST_DURATION, rawDuration)
        : Math.max(0, rawDuration);
      const keySource = raw.key ?? options.key;
      const key = keySource === undefined
        ? this.toastKey(toastType, title, fullText || title)
        : this.toastKey(keySource);
      const now = Date.now();

      this._toastSequence = (this._toastSequence || 0) + 1;

      return {
        id: `${now}-${this._toastSequence}`,
        type: toastType,
        title,
        message,
        details,
        fullText: fullText || title,
        previewText: preview.text,
        expanded: false,
        expandable: preview.truncated,
        duration: Math.max(0, toastDuration),
        remainingMs: Math.max(0, toastDuration),
        repeatCount: 1,
        key,
        createdAt: now,
        lastSeenAt: now,
        startedAt: 0,
        timerId: null,
        paused: false,
        animationNonce: 0,
      };
    },

    _findDuplicateToast(key, now) {
      if (!key) return null;
      return [...this.toasts, ...this.toastQueue].find((toast) =>
        toast.key === key && now - (toast.lastSeenAt || toast.createdAt || 0) <= TOAST_DEDUPE_WINDOW_MS
      ) || null;
    },

    _mergeDuplicateToast(existing, incoming) {
      existing.repeatCount = (existing.repeatCount || 1) + 1;
      existing.lastSeenAt = Date.now();
      existing.duration = Math.max(existing.duration || 0, incoming.duration || 0);
      existing.remainingMs = existing.duration;

      if (this.toasts.some((toast) => toast.id === existing.id) && !existing.expanded && !existing.paused) {
        this._startToastTimer(existing, true);
      }
      this._syncToastCollections();
    },

    _startToastTimer(toast, resetRemaining = false) {
      this._clearToastTimer(toast);
      if (!toast.duration) {
        toast.remainingMs = 0;
        return;
      }

      if (resetRemaining || !toast.remainingMs || toast.remainingMs > toast.duration) {
        toast.remainingMs = toast.duration;
      }
      toast.paused = false;
      toast.startedAt = Date.now();
      toast.animationNonce = (toast.animationNonce || 0) + 1;
      toast.timerId = setTimeout(() => this.dismissToast(toast.id), toast.remainingMs);
      this._syncToastCollections();
    },

    _clearToastTimer(toast) {
      if (toast?.timerId) clearTimeout(toast.timerId);
      if (toast) toast.timerId = null;
    },

    _promoteQueuedToasts() {
      while (this.toasts.length < TOAST_MAX_VISIBLE && this.toastQueue.length > 0) {
        const [next, ...rest] = this.toastQueue;
        this.toastQueue = rest;
        this.toasts = [...this.toasts, next];
        this._startToastTimer(next, true);
      }

      if (this.toastQueue.length === 0 && this.toastOverflowCount > 0 && this.toasts.length < TOAST_MAX_VISIBLE) {
        const collapsed = this.toastOverflowCount;
        this.toastOverflowCount = 0;
        const summary = this._normalizeToast({
          title: 'Notifications collapsed',
          message: `${collapsed} additional notification${collapsed === 1 ? '' : 's'} were hidden while the toast queue was full.`,
          type: 'info',
          duration: 5000,
          key: `toast-overflow-summary:${Date.now()}`,
        }, 'info', 5000);
        this.toasts = [...this.toasts, summary];
        this._startToastTimer(summary, true);
      }
    },

    _syncToastCollections() {
      this.toasts = [...this.toasts];
      this.toastQueue = [...this.toastQueue];
    },
  },
};

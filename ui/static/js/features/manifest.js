// manifest.js — fetches the backend UI manifest (enum lists, agent field
// specs, category colors) once on init and exposes it to all features. The
// goal is to have one source of truth for option lists and palette so adding
// a new sync mode, auth mode, or notification provider stops touching the
// frontend at all.
//
// Lookup helpers (manifestEnumLabel, manifestEnumDescription, manifestAgent,
// manifestCategory) are all defensive: if the manifest hasn't loaded yet
// (network race, fetch failed) they return sensible fallbacks so templates
// keep rendering rather than throwing on undefined access.

const EMPTY_MANIFEST = {
  appTypes: [],
  syncBehaviorAddModes: [],
  syncBehaviorRemoveModes: [],
  syncBehaviorResetModes: [],
  authModes: [],
  authRequiredModes: [],
  pullIntervalPresets: [],
  sessionTtlBounds: { min: 1, max: 365 },
  cfCategories: [],
  profileGroups: [],
  notificationAgents: [],
};

export default {
  state: {
    manifest: EMPTY_MANIFEST,
    manifestLoaded: false,
    // Pre-built lookup map: human-readable category/group name → CSS class
    // suffix. Populated by loadManifest() so getCategoryClass() and similar
    // helpers don't have to scan the categories array on every call.
    _categoryClassMap: {},
    _profileGroupClassMap: {},
  },

  methods: {
    async loadManifest() {
      try {
        const r = await fetch('/api/ui/manifest');
        if (!r.ok) return;
        const m = await r.json();
        this.manifest = m;
        this.manifestLoaded = true;
        this._buildCategoryLookup();
        this._applyCategoryColors();
      } catch (e) {
        console.error('loadManifest:', e);
      }
    },

    // Build label-to-CSS-class lookup tables. The class suffix is the ID
    // (cat-anime, grp-sqp, …) and aliases collapse near-duplicate labels
    // ("French Audio Version" + "French HQ Source Groups" → cat-french).
    _buildCategoryLookup() {
      const cats = {};
      for (const c of this.manifest.cfCategories || []) {
        cats[c.label] = `cat-${c.id}`;
        for (const a of (c.aliases || [])) cats[a] = `cat-${c.id}`;
      }
      this._categoryClassMap = cats;

      const grps = {};
      for (const g of this.manifest.profileGroups || []) {
        grps[g.label] = `grp-${g.id}`;
        for (const a of (g.aliases || [])) grps[a] = `grp-${g.id}`;
      }
      this._profileGroupClassMap = grps;
    },

    // Push category/group colors into CSS custom properties on :root.
    // tokens.css declares matching --cat-* / --grp-* fallbacks so the UI
    // looks correct even if loadManifest hasn't finished; this overrides
    // them with the canonical values from Go.
    _applyCategoryColors() {
      const root = document.documentElement.style;
      for (const c of this.manifest.cfCategories || []) {
        if (c.color) root.setProperty(`--cat-${c.id}`, c.color);
      }
      for (const g of this.manifest.profileGroups || []) {
        if (g.color) root.setProperty(`--grp-${g.id}`, g.color);
      }
    },

    // Look up the label for an enum value. Returns the value itself when
    // unknown so a stale or misconfigured value still displays something.
    manifestEnumLabel(enumName, value) {
      const list = this.manifest[enumName];
      if (!list) return value;
      const m = list.find(v => v.value === value);
      return m ? m.label : value;
    },

    // Look up helper-text/description for an enum value (used by sync-modal
    // dropdowns to render an explanation paragraph below the select).
    manifestEnumDescription(enumName, value) {
      const list = this.manifest[enumName];
      if (!list) return '';
      const m = list.find(v => v.value === value);
      return (m && m.description) || '';
    },

    // Returns the AgentTypeMeta for the given provider type, or null.
    manifestAgent(type) {
      return (this.manifest.notificationAgents || []).find(a => a.type === type) || null;
    },

    // Returns the CSS class suffix (cat-anime, cat-other, …) for a TRaSH
    // category name. Falls back to cat-other when unknown or before
    // manifest loads.
    manifestCategoryClass(label) {
      return this._categoryClassMap[label] || 'cat-other';
    },

    // Same as above for profile groups (grp-sqp, grp-other, …).
    manifestProfileGroupClass(label) {
      return this._profileGroupClassMap[label] || 'grp-other';
    },
  },
};

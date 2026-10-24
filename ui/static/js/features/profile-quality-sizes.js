// Per-profile quality sizes: the profile editor's Quality size tab.
//
// Sonarr v5 moved quality sizes off the instance-wide list under Media
// Management and onto each quality profile. On those versions the instance tab
// has nothing to write, so the choice lives here instead: one source per
// profile, plus the user's own numbers for individual qualities.
//
// The tab is shown only when the rule's instance keeps sizes per profile,
// which the backend answers from the shape of the instance's definition list.
// Never gated on the app being Sonarr: that would put the tab on Sonarr 4
// where it cannot work, and would miss the day Radarr does the same thing.

const QS_FIELDS = ['min', 'preferred', 'max'];

export default {
  state: {
    // Loaded per rule from GET /api/auto-sync/rules/{id}/quality-sizes.
    pdQSPerProfile: false,
    pdQSRows: [],
    pdQSSets: [],
    pdQSDefaultSetType: '',
    pdQSLoading: false,
    pdQSLoadedKey: null,
    // Editor state, saved with the rule.
    pdQSSource: '',
    pdQSOverrides: {},
    // The default view lists the qualities this profile allows. Measured
    // reason: a guide profile like WEB-1080p allows 2 of 22, so listing
    // everything would show 22 rows of inputs to set 2.
    pdQSShowAll: false,
    pdQSSaveSetName: '',
    // Which instance the numbers belong to, answered by the backend. In a
    // mixed Sonarr 4 and 5 setup the editor is bound to whichever instance
    // comes first in the config, which is not a decision, so the tab names
    // the one it actually resolved against.
    pdQSInstanceName: '',
    pdQSInstanceId: '',
    pdQSInstanceSwitched: false,
    // True when "on the instance" is the app's blank-profile template rather
    // than a profile that exists. The honest wording for a profile the user
    // has not created yet.
    pdQSFromSchema: false,
    pdQSWarning: '',
    // True when no instance of this type could be reached, so whether sizes
    // live on the profile is unknown. The tab is shown saying that, because
    // hiding it would claim there is nothing to set.
    pdQSUnknown: false,
    // Set on the first edit in this tab. Until then the saved rule is the
    // truth; afterwards the editor's own state is, so a reload must not
    // overwrite an unsaved change.
    _pdQSTouched: false,
  },

  methods: {
    // --- Loading ---

    async pdQSLoad(force = false) {
      const app = this.pdQSAppType();
      if (!app) {
        this.pdQSDropTab();
        return;
      }
      // Keyed on the target, not on a rule. A rule only exists after the
      // first sync, so keying on one meant the tab could not appear until the
      // user had synced the profile once with no sizes, gone back in to set
      // them, and synced again.
      const arrId = this.pdQSArrProfileId();
      const prefer = this.pdQSPreferredInstanceId();
      const fixed = this.pdQSInstanceFixed();
      const key = `${app}|${prefer}|${fixed}|${arrId}`;
      if (!force && this.pdQSLoadedKey === key) return;
      // The backend may have to read the definition list from more than one
      // instance plus a profile, so this is slow enough to lose a race with
      // the user opening another profile. Drop an answer that no longer
      // matches what the editor is asking about.
      this.pdQSLoading = true;
      try {
        const q = new URLSearchParams({ app, arrProfileId: String(arrId) });
        if (prefer) q.set('instanceId', prefer);
        // Lets the backend answer for this rule's own instance instead of
        // resolving past it to one that does keep sizes per profile.
        if (fixed) q.set('instanceFixed', '1');
        // Lets the backend mark which qualities this guide profile allows
        // while it does not exist on the instance yet.
        const trashId = this.profileDetail?.profile?.trashId;
        if (trashId) q.set('trashId', trashId);
        const r = await fetch(`/api/profile-quality-sizes?${q}`);
        const stillCurrent = () =>
          `${this.pdQSAppType()}|${this.pdQSPreferredInstanceId()}|${this.pdQSInstanceFixed()}|${this.pdQSArrProfileId()}` === key;
        if (!stillCurrent()) return;
        if (!r.ok) {
          // An instance that is down, or a profile it no longer has. Say
          // nothing rather than show an empty tab that reads as "this profile
          // has no qualities".
          this.pdQSDropTab();
          return;
        }
        const d = await r.json();
        if (!stillCurrent()) return;
        this.pdQSPerProfile = !!d.perProfileSizes;
        this.pdQSUnknown = !!d.unknown;
        // Switching the target to an instance that keeps sizes in the shared
        // list takes the tab away. Leave it rather than leaving its panel on
        // screen with no tab to go back to. An unreachable instance keeps the
        // tab, so it must not be kicked out of here: checking only
        // perProfile bounced the user straight back out of a tab that was
        // showing them why it could not load.
        if (!this.pdQSShowTab() && this.spActiveTab === 'quality-size') {
          this.spActiveTab = 'default';
        }
        this.pdQSRows = d.rows || [];
        this.pdQSSets = d.sets || [];
        this.pdQSDefaultSetType = d.defaultSetType || '';
        this.pdQSInstanceName = d.instanceName || '';
        this.pdQSInstanceId = d.instanceId || '';
        this.pdQSInstanceSwitched = !!d.instanceSwitched;
        this.pdQSFromSchema = !!d.fromSchema;
        this.pdQSWarning = d.warning || '';
        this.pdQSLoadedKey = key;
        // The rule on disk is the truth on open; the editor's own state takes
        // over from the first edit.
        if (this.pdQSSource === '' && !this._pdQSTouched) {
          this.pdQSSource = d.source || '';
          this.pdQSOverrides = { ...(d.overrides || {}) };
        }
      } catch (e) {
        console.error('pdQSLoad:', e);
      } finally {
        this.pdQSLoading = false;
      }
    },

    // Which app the open profile belongs to.
    pdQSAppType() {
      return this.profileDetail?.instance?.type
        || this.profileDetail?.profile?.appType
        || '';
    },

    // The Arr profile the editor is aimed at, or 0 for one that does not
    // exist yet. The locked id wins, because that is the edit-an-existing-rule
    // flow; the sync form's value covers "target this unmanaged profile".
    pdQSArrProfileId() {
      const locked = this.profileDetail?._editLockedArrProfileId;
      if (locked) return locked;
      // syncForm is only authoritative while the sync modal is open. The
      // modal rebuilds it from scratch on open, so before that its fields
      // still hold the *previous* profile's target: browsing a guide profile
      // after syncing another one sent that one's id, on the other instance.
      if (!this.showSyncModal) return 0;
      const n = parseInt(this.syncForm?.arrProfileId, 10);
      return Number.isFinite(n) && n > 0 ? n : 0;
    },

    // The instance to prefer. The sync modal is where the target actually
    // becomes real, so its choice wins once it has one; before that the
    // editor's own binding is the best guess, and the backend is free to
    // answer for a different instance of the same app when that one keeps its
    // sizes in the shared list.
    pdQSPreferredInstanceId() {
      // A saved rule's own instance wins. Otherwise the sync step's pick,
      // while that step is open, and the editor's binding before it.
      if (this.pdQSInstanceFixed()) return this.profileDetail?.instance?.id || '';
      if (this.showSyncModal && this.syncForm?.instanceId) return this.syncForm.instanceId;
      return this.profileDetail?.instance?.id || '';
    },

    // Whether this editor is bound to an instance that is already decided,
    // which means a saved rule. Picking a target in the sync step is not a
    // decision yet: closing that step and syncing somewhere else must not
    // have cost the user this tab in the meantime.
    pdQSInstanceFixed() {
      return !!this.profileDetail?._editLockedArrProfileId;
    },

    // The name a rerun from history gives the sizes it restores, when the set
    // it used has been deleted since. Composed here rather than on the server
    // so the date reads in the viewer's own format, and keyed on the entry's
    // own timestamp so rerunning the same entry twice reuses one set instead
    // of stacking copies.
    qsRestoredSetName(sh) {
      const when = sh?.lastSync || sh?.appliedAt;
      const day = when ? new Date(when).toLocaleDateString() : '';
      const was = (sh?.qualitySizeSetName || '').trim();
      if (was && was !== 'a set that no longer exists') {
        return day ? `${was} (restored ${day})` : `${was} (restored)`;
      }
      return day ? `Restored sizes ${day}` : 'Restored sizes';
    },

    pdQSShowTab() {
      return !!this.pdQSPerProfile || !!this.pdQSUnknown;
    },

    pdQSDropTab() {
      this.pdQSPerProfile = false;
      this.pdQSUnknown = false;
      this.pdQSRows = [];
      if (this.spActiveTab === 'quality-size') this.spActiveTab = 'default';
    },

    // --- The source selector ---

    // The set the source currently names, or null for off. Read from the
    // sets the endpoint shipped, which carry their own rows, so everything
    // below reflects what the user just picked rather than what was saved.
    pdQSActiveSet() {
      const src = this.pdQSSource;
      if (!src || src === 'off') return null;
      return (this.pdQSSets || []).find(s => s.value === src) || null;
    },

    // This set's entry for one quality, or null when it does not name it.
    // A set covering part of the profile is the normal case: the guide's
    // series set covers 14 of a v5 profile's 22 qualities.
    pdQSEntryFor(quality) {
      const set = this.pdQSActiveSet();
      if (!set) return null;
      return (set.entries || []).find(e => e.quality === quality) || null;
    },

    pdQSSourceLabel() {
      const src = this.pdQSSource;
      if (!src || src === 'off') return 'Off';
      const set = (this.pdQSSets || []).find(s => s.value === src);
      if (set) return set.origin === 'user' ? set.name : this.pdQSSetTitle(set.name);
      // A set that no longer exists: a user set deleted elsewhere, or a guide
      // set the guide dropped. Nothing gets written, so say that rather than
      // show a dead id.
      return 'Not found, nothing is written';
    },

    // The guide names its sets by type. "series" and "movie" read better with
    // a capital, and the sqp ones are known by their own spelling.
    pdQSSetTitle(type) {
      if (type === 'sqp-streaming') return 'SQP Streaming';
      if (type === 'sqp-uhd') return 'SQP UHD';
      return (type || '').charAt(0).toUpperCase() + (type || '').slice(1);
    },

    pdQSSetSource(value) {
      this.pdQSSource = value;
      this._pdQSTouched = true;
    },

    // The app's own default set, offered as the one-click action on a rule
    // sitting at off. Visible and chosen, never written behind the user.
    pdQSDefaultSet() {
      return (this.pdQSSets || []).find(
        s => s.origin === 'guide' && s.name === this.pdQSDefaultSetType
      ) || null;
    },

    // --- Rows ---

    // Default to what the profile allows, with a show-all switch. A size on a
    // quality the profile does not allow changes nothing, because a
    // disallowed quality is never downloaded. That argument only holds per
    // profile: on the shared list a size mattered if any profile allowed it.
    //
    // Allowed qualities the set does not cover still belong here, showing the
    // instance's own value, because that is exactly where someone might want
    // to set something themselves.
    // The qualities the editor currently allows, as leaf name to bool, or null
    // when it has nothing to say and the row's own flag stands.
    //
    // Read live from the editor rather than taken from the response, because
    // the Qualities editor changes this without a save: switching Bluray on
    // has to put a Bluray row here straight away. The backend's flags still
    // carry the view until the profile detail has loaded, which happens after
    // this tab's first request.
    //
    // Same leaf-flattening the overview diff uses: a group's members inherit
    // the group's state.
    pdQSAllowedNames() {
      const flatten = (items) => {
        const m = new Map();
        for (const it of (items || [])) {
          if (it.items && it.items.length > 0) {
            for (const leaf of it.items) {
              const name = typeof leaf === 'string' ? leaf : (leaf?.name || leaf?.quality?.name);
              if (name) m.set(name, !!it.allowed);
            }
            continue;
          }
          const name = it.name || it.quality?.name;
          if (name) m.set(name, !!it.allowed);
        }
        return m;
      };
      if (this.qualityStructure && this.qualityStructure.length > 0) {
        return flatten(this.qualityStructure);
      }
      const base = flatten(this.profileDetail?.detail?.profile?.items);
      if (!base.size) return null;
      // The legacy flat map, still what a rule saved before qualityStructure
      // carries.
      for (const [name, allowed] of Object.entries(this.qualityOverrides || {})) {
        base.set(name, !!allowed);
      }
      return base;
    },

    pdQSRowAllowed(row) {
      const names = this.pdQSAllowedNames();
      return names ? !!names.get(row.quality) : !!row.allowed;
    },

    // Rows built without the instance, for when it could not be reached. The
    // qualities and their order come from the profile the editor already has
    // from the guide; the numbers come from the chosen set, which is also
    // guide or config data. So the user can still see what will be written.
    //
    // "On the instance" is left empty on purpose and rendered as N/A. The last
    // sync's values are the obvious thing to put there and the wrong one: we
    // cannot check whether anything has drifted since, so showing them as
    // current would state something we have not verified.
    pdQSOfflineRows() {
      const set = this.pdQSActiveSet();
      const byName = new Map();
      for (const e of (set?.entries || [])) byName.set(e.quality, e);
      const order = [];
      const seen = new Set();
      const walk = (items) => {
        for (const it of (items || [])) {
          if (it.items && it.items.length > 0) {
            const group = it.name || it.quality?.name || '';
            for (const leaf of it.items) {
              const name = typeof leaf === 'string' ? leaf : (leaf?.name || leaf?.quality?.name);
              if (name && !seen.has(name)) { seen.add(name); order.push({ quality: name, group }); }
            }
            continue;
          }
          const name = it.name || it.quality?.name;
          if (name && !seen.has(name)) { seen.add(name); order.push({ quality: name, group: '' }); }
        }
      };
      walk(this.qualityStructure && this.qualityStructure.length > 0
        ? this.qualityStructure
        : this.profileDetail?.detail?.profile?.items);
      // Anything the set or an override names that the profile does not list.
      for (const name of [...byName.keys(), ...Object.keys(this.pdQSOverrides || {})]) {
        if (!seen.has(name)) { seen.add(name); order.push({ quality: name, group: '' }); }
      }
      // Stored lowest priority first, same as the profile; the table reads
      // highest first, which is the order both Arr UIs show.
      order.reverse();
      return order.map(r => ({
        quality: r.quality,
        group: r.group,
        allowed: true, // pdQSRowAllowed answers this from the editor's own state
        current: {},
      }));
    },

    pdQSVisibleRows() {
      const rows = (this.pdQSUnknown && (this.pdQSRows || []).length === 0)
        ? this.pdQSOfflineRows()
        : (this.pdQSRows || []);
      if (this.pdQSShowAll) return rows;
      const allowed = rows.filter(row => this.pdQSRowAllowed(row) || this.pdQSIsCustom(row.quality));
      if (allowed.length) return allowed;
      // Nothing is marked allowed, which is exactly what the blank-profile
      // template looks like: the app ships it with every quality off. A
      // filter that removes every row is a dead end rather than a filter, and
      // an empty table reads as "there is nothing to set here".
      //
      // Fall back to the qualities the chosen set covers, so picking Series
      // or Anime brings its rows into view, and to everything when no set is
      // chosen. On a profile that does exist this never runs, so the tight
      // default view there is unchanged.
      const covered = rows.filter(
        row => this.pdQSEntryFor(row.quality) || this.pdQSIsCustom(row.quality)
      );
      return covered.length ? covered : rows;
    },

    pdQSHiddenCount() {
      return (this.pdQSRows || []).length - this.pdQSVisibleRows().length;
    },

    // Where this row's number comes from. The user needs this to know which
    // lever to pull: a value from the app default looks identical to one
    // chosen for this profile unless the tab says so.
    pdQSRowSource(row) {
      // The word the user pressed, and a peer of the set names listed in the
      // same column: Series, Anime, a saved set, or Custom.
      if (this.pdQSIsCustom(row.quality)) return 'Custom';
      const set = this.pdQSActiveSet();
      if (!set) return 'Not written';
      if (!this.pdQSEntryFor(row.quality)) return 'Not in this set, left as is';
      return set.origin === 'user' ? set.name : this.pdQSSetTitle(set.name);
    },

    pdQSCurrentText(row) {
      // Not "unlimited", and not the last sync's numbers: with the instance
      // unreachable we do not know what it holds now.
      if (this.pdQSUnknown) return 'N/A';
      const parts = QS_FIELDS.map(f => {
        const v = row.current?.[f];
        // The app stores no value for Unlimited, and its own UI shows the
        // word rather than a number.
        return v === null || v === undefined ? '∞' : v;
      });
      return parts.join(' / ');
    },

    // Resolved client-side in the same order the sync engine uses: an
    // override, then the chosen set, then nothing. Not read from the row's
    // resolved field, which the backend computed from the *saved* rule and
    // would show the previous set's numbers under the new set's name.
    pdQSResolvedText(row) {
      if (this.pdQSIsCustom(row.quality)) {
        const ov = this.pdQSOverrides[row.quality];
        return QS_FIELDS.map(f => ov?.[f] ?? 0).join(' / ');
      }
      const entry = this.pdQSEntryFor(row.quality);
      if (!entry) return '-';
      return QS_FIELDS.map(f => entry[f]).join(' / ');
    },

    // --- Per-quality overrides ---

    pdQSIsCustom(quality) {
      return !!(this.pdQSOverrides || {})[quality];
    },

    pdQSToggleCustom(row) {
      const next = { ...(this.pdQSOverrides || {}) };
      if (next[row.quality]) {
        delete next[row.quality];
      } else {
        // Prefill with whatever the row would get today: the chosen set's
        // entry when it has one, otherwise the instance's own values. A null
        // means the instance has no value, which is its limit rather than 0,
        // so it must not prefill 0 and silently cap everything.
        const src = this.pdQSEntryFor(row.quality) || row.current;
        const lim = this.pdQSLimits();
        next[row.quality] = {
          min: src?.min ?? 0,
          preferred: src?.preferred ?? lim.preferred,
          max: src?.max ?? lim.max,
        };
      }
      this.pdQSOverrides = next;
      this._pdQSTouched = true;
    },

    pdQSOverrideVal(quality, field) {
      return (this.pdQSOverrides || {})[quality]?.[field] ?? 0;
    },

    pdQSSetOverrideVal(quality, field, value) {
      const next = { ...(this.pdQSOverrides || {}) };
      if (!next[quality]) return;
      const n = parseFloat(value);
      next[quality] = { ...next[quality], [field]: Number.isFinite(n) ? n : 0 };
      this.pdQSOverrides = next;
      this._pdQSTouched = true;
    },

    // The app's ceilings, used when prefilling an Unlimited field. Sonarr and
    // Radarr differ, and the numbers are the same ones the instance-wide tab
    // uses.
    pdQSLimits() {
      const app = this.profileDetail?.instance?.type;
      return app === 'radarr'
        ? { preferred: 1999, max: 2000 }
        : { preferred: 995, max: 1000 };
    },

    pdQSChangeCount() {
      return Object.keys(this.pdQSOverrides || {}).length;
    },

    // True when nothing on screen can follow the chosen set, because every
    // row has its own value. Deleting a set leaves a rule exactly here, and
    // without saying so the source picker reads as broken.
    pdQSAllRowsCustom() {
      const rows = this.pdQSVisibleRows();
      return rows.length > 0 && rows.every(row => this.pdQSIsCustom(row.quality));
    },

    // Back to following the set, keeping the source. pdQSReset() clears the
    // source as well, which is a different intent.
    pdQSClearOverrides() {
      this.pdQSOverrides = {};
      this._pdQSTouched = true;
    },

    // What the basics-strip row shows.
    pdQSSummary() {
      const n = this.pdQSChangeCount();
      const base = this.pdQSSourceLabel();
      // An override is written even with no set chosen, so "Off" on its own
      // would understate what the next sync does.
      return n > 0 ? `${base} · ${n} change${n === 1 ? '' : 's'}` : base;
    },

    // 'off' and not '': an empty source is indistinguishable from "the field
    // was not sent", which every partial rule writer relies on, so an empty
    // one could never be saved and this button did nothing.
    pdQSReset() {
      this.pdQSSource = 'off';
      this.pdQSOverrides = {};
      this._pdQSTouched = true;
    },

    async pdQSDeleteSet(set) {
      if (!set || set.origin !== 'user' || !set.id) return;
      // Say what will happen, then do that. The dialog used to describe
      // deleting and the server then refused, so the warning described an
      // outcome it would not allow.
      const usedBy = set.usedBy || [];
      const message = usedBy.length > 0
        ? `"${set.name}" is removed from the list. The sync rules using it, on ${usedBy.join(', ')}, keep writing these exact sizes, so nothing changes on any instance.`
        : `"${set.name}" is removed from the list for good. No sync rule is using it, so nothing changes on any instance.`;
      const ok = await new Promise(resolve => {
        this.confirmModal = {
          show: true,
          title: 'Delete this size set?',
          message,
          confirmLabel: 'Delete',
          onConfirm: () => resolve(true),
          onCancel: () => resolve(false),
        };
      });
      if (!ok) return;
      try {
        const r = await fetch(`/api/quality-size-sets/${set.id}`, { method: 'DELETE' });
        const d = await r.json().catch(() => ({}));
        if (!r.ok) {
          // The backend refuses while a rule still points at it and names
          // the instances, so the message is worth showing verbatim.
          this.showToast(d.error || 'Could not delete the set', 'error');
          return;
        }
        this.pdQSSets = (this.pdQSSets || []).filter(x => x.value !== set.value);
        if (this.pdQSSource === set.value) {
          // Re-read what the server stored for this rule rather than working
          // out the same conversion again here. Two copies of that rule would
          // drift, and the editor would then show something the saved rule
          // does not say.
          this.pdQSSource = '';
          this.pdQSOverrides = {};
          this._pdQSTouched = false;
          this.pdQSLoadedKey = null;
          await this.pdQSLoad(true);
        }
        const kept = d.convertedRules || 0;
        const landed = (d.landedOn || '').startsWith('guide:')
          ? `now follow the ${this.pdQSSetTitle(this.pdQSDefaultSetType)} sizes, with any differing values kept as custom`
          : 'kept the sizes as their own values';
        this.showToast(
          kept > 0
            ? `Deleted "${set.name}". ${kept} sync rule${kept === 1 ? '' : 's'} ${landed}.`
            : `Deleted "${set.name}"`,
          'success');
      } catch (e) {
        console.error('pdQSDeleteSet:', e);
        this.showToast('Could not delete the set', 'error');
      }
    },

    // --- Saving the current table as a reusable set ---

    // A one-time action, not a standing link: it writes out the resolved
    // table under a name and points the rule at it. Editing the set later
    // does not reach back into a rule that merely happened to be its source.
    async pdQSSaveAsSet() {
      const name = (this.pdQSSaveSetName || '').trim();
      if (!name) {
        this.showToast('Give the set a name first', 'error');
        return;
      }
      const app = this.profileDetail?.instance?.type;
      // Built from the same client-side resolution the table shows, so the
      // saved set is what the user is looking at. Reading row.resolved here
      // took the *saved* rule's numbers, which meant picking a new set and
      // saving it wrote the previous set's values under the new name.
      const qualities = [];
      for (const row of this.pdQSRows || []) {
        const v = this.pdQSIsCustom(row.quality)
          ? this.pdQSOverrides[row.quality]
          : this.pdQSEntryFor(row.quality);
        if (!v) continue;
        qualities.push({
          quality: row.quality,
          min: v.min ?? 0,
          preferred: v.preferred ?? 0,
          max: v.max ?? 0,
        });
      }
      if (qualities.length === 0) {
        this.showToast('There are no sizes to save yet', 'error');
        return;
      }
      try {
        const r = await fetch('/api/quality-size-sets', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ name, appType: app, qualities }),
        });
        const d = await r.json();
        if (!r.ok) {
          this.showToast(d.error || 'Could not save the set', 'error');
          return;
        }
        // Point the rule at the new set and drop the overrides it just baked
        // in, so the same numbers are not stored in two places.
        this.pdQSSets = [...(this.pdQSSets || []), {
          value: `custom:${d.id}`, name: d.name, origin: 'user', id: d.id, entries: qualities,
        }];
        this.pdQSSource = `custom:${d.id}`;
        this.pdQSOverrides = {};
        this.pdQSSaveSetName = '';
        this._pdQSTouched = true;
        this.showToast(`Saved "${name}" and pointed this profile at it`, 'success');
      } catch (e) {
        console.error('pdQSSaveAsSet:', e);
        this.showToast('Could not save the set', 'error');
      }
    },
  },
};

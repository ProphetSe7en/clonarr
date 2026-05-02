export default {
  state: {},
  methods: {
    async loadCFBrowse(appType) {
      try {
        const [cfsRes, groupsRes, customRes] = await Promise.all([
          fetch(`/api/trash/${appType}/cfs`),
          fetch(`/api/trash/${appType}/cf-groups`),
          fetch(`/api/custom-cfs/${appType}`)
        ]);
        if (!cfsRes.ok || !groupsRes.ok) return;
        const cfs = await cfsRes.json();
        const groups = await groupsRes.json();
        const customCFs = customRes.ok ? await customRes.json() : [];
        this.cfBrowseData = { ...this.cfBrowseData, [appType]: { cfs, groups, customCFs } };
      } catch (e) { /* not yet cloned */ }
    },

    async loadConflicts(appType) {
      try {
        const res = await fetch(`/api/trash/${appType}/conflicts`);
        if (res.ok) this.conflictsData = { ...this.conflictsData, [appType]: await res.json() };
      } catch (e) { /* not available */ }
    },

    getCFBrowseGroups(appType) {
      const data = this.cfBrowseData[appType];
      if (!data) return [];

      // Build CF lookup by trash_id
      const cfMap = {};
      for (const cf of data.cfs) {
        cfMap[cf.trash_id] = cf;
      }

      // Each TRaSH group file becomes its own top-level category
      const categories = [];
      const usedCFIds = new Set();

      for (const group of data.groups) {
        let prefix = '', shortName = '';
        if (group.name.startsWith('[')) {
          const idx = group.name.indexOf(']');
          if (idx > 0) {
            prefix = group.name.substring(1, idx).trim();
            shortName = group.name.substring(idx + 1).trim();
          }
        }
        // Remap prefixes
        if (prefix === 'Required') prefix = 'Golden Rule';
        if (prefix === 'SQP') prefix = 'Miscellaneous';
        // Display name: use shortName if present, otherwise prefix, otherwise full name
        const displayName = shortName ? (prefix + ' — ' + shortName) : (prefix || group.name);
        // Category class uses the prefix for color matching
        const categoryClass = prefix || 'Other';

        const cfs = [];
        for (const cfEntry of (group.custom_formats || [])) {
          usedCFIds.add(cfEntry.trash_id);
          const cf = cfMap[cfEntry.trash_id];
          cfs.push({
            trashId: cfEntry.trash_id,
            name: cfEntry.name || cf?.name || cfEntry.trash_id,
            description: cf?.description || '',
            score: cf?.trash_scores?.default,
          });
        }

        if (cfs.length > 0) {
          categories.push({
            category: categoryClass,
            displayName,
            // Carry group integer through for the new sort. Falsy / null when
            // the cf-group JSON has no `group` field set.
            groupNum: (group.group ?? null),
            isCustom: false,
            groups: [{ name: group.name, shortName: shortName || displayName, cfs }],
            totalCFs: cfs.length,
            trashDescription: group.trash_description || '',
          });
        }
      }

      // CFs not in any TRaSH group go into "Other"
      const ungrouped = [];
      for (const cf of data.cfs) {
        if (!usedCFIds.has(cf.trash_id)) {
          ungrouped.push({ trashId: cf.trash_id, name: cf.name, description: cf.description || '', score: cf.trash_scores?.default });
        }
      }
      if (ungrouped.length > 0) {
        ungrouped.sort((a, b) => a.name.localeCompare(b.name));
        categories.push({ category: 'Other', displayName: 'Other', groupNum: null, isCustom: false, groups: [{ name: 'Other', shortName: 'Other', cfs: ungrouped }], totalCFs: ungrouped.length });
      }

      // Inject custom CFs
      const customCFs = data.customCFs || [];
      if (customCFs.length > 0) {
        const allCustomCFs = customCFs.map(ccf => ({ trashId: ccf.id, name: ccf.name, description: '', score: undefined, isCustom: true }));
        allCustomCFs.sort((a, b) => a.name.localeCompare(b.name));
        categories.push({ category: 'Custom', displayName: 'Custom', groupNum: null, isCustom: true, groups: [{ name: 'Custom Formats', shortName: 'Custom Formats', cfs: allCustomCFs }], totalCFs: allCustomCFs.length });
      }

      // Group-integer sort (see _compareCFGroups): cf-groups with explicit
      // `group` field sort first by integer, then "Other" tier, then "Custom"
      // tier last. Display-name alphabetical tiebreak within tiers.
      return categories.sort((a, b) =>
        this._compareCFGroups(a.displayName, a.groupNum, !!a.isCustom,
                              b.displayName, b.groupNum, !!b.isCustom));
    },

    // --- CF Editor (Create/Edit) ---

    // True when the name typed in the CF Editor is byte-exact match
    // against a TRaSH-published CF for the same app. Drives the small
    // "guide" badge next to the Name field. Save is NEVER blocked —
    // the user owns naming. The badge is informational only; the real
    // cross-usage detection runs at sync-plan time.
    get cfEditorTrashMatch() {
      const name = (this.cfEditorForm?.name || '').trim();
      if (!name) return false;
      const appType = this.cfEditorForm?.appType;
      const cfs = this.cfBrowseData?.[appType]?.cfs || [];
      return cfs.some(c => c.name === name);
    },

    async openCFEditor(mode, appType, existingCF = null) {
      this.cfEditorMode = mode;
      this.cfEditorResult = null;
      this.cfEditorSaving = false;
      this.cfEditorShowPreview = false;
      this.cfEditorSpecCounter = 0;

      // Set appType first so loadCFEditorSchema can read it
      this.cfEditorForm.appType = appType;
      await this.loadCFEditorSchema();

      if (mode === 'edit' && existingCF) {
        // Load full custom CF data from API
        let allCFs;
        try {
          const res = await fetch(`/api/custom-cfs/${appType}`);
          allCFs = await res.json();
        } catch (e) {
          this.showToast('Could not load custom CF data: ' + e.message, 'error', 8000);
          return;
        }
        const full = (allCFs || []).find(c => c.id === existingCF.trashId);
        if (!full) {
          this.showToast('Custom CF not found — it may have been deleted', 'error', 8000);
          return;
        }
        this.cfEditorForm = {
          id: full.id,
          name: full.name,
          appType: full.appType,
          category: full.category || 'Custom',
          newCategory: '',
          includeInRename: full.includeInRename || false,
          specifications: (full.specifications || []).map(s => this.arrSpecToEditorSpec(s)),
          trashId: full.trashId || '',
          trashScores: Object.entries(full.trashScores || {}).map(([k,v]) => ({context:k, score:v})),
          description: full.description || '',
        };
      } else {
        this.cfEditorForm = {
          id: '',
          name: '',
          appType: appType,
          category: 'Custom',
          newCategory: '',
          includeInRename: false,
          specifications: [],
          trashId: '',
          trashScores: [],
          description: '',
        };
      }

      // Force Alpine reactivity on form object (needed for x-model on nested selects)
      this.cfEditorForm = { ...this.cfEditorForm };
      this.showCFEditor = true;
    },

    // Convert Arr API specification to editor format.
    // Matches fields against the loaded schema to restore dropdowns, checkboxes, etc.
    // Without this, Language specs show "value: 3" instead of a dropdown on edit.
    arrSpecToEditorSpec(arrSpec) {
      let fields = [];
      // Parse raw fields from the stored spec
      let rawFields = {};
      if (arrSpec.fields) {
        let parsed = arrSpec.fields;
        if (typeof parsed === 'string') {
          try { parsed = JSON.parse(parsed); } catch(e) { parsed = []; }
        }
        if (Array.isArray(parsed)) {
          for (const f of parsed) rawFields[f.name] = f.value;
        } else if (typeof parsed === 'object') {
          rawFields = { ...parsed };
        }
      }
      // Try to match against schema for this implementation type
      const schema = (this.cfEditorSchema[this.cfEditorForm.appType] || [])
        .find(s => s.implementation === arrSpec.implementation);
      if (schema) {
        fields = schema.fields.map(f => {
          let val = rawFields[f.name] !== undefined ? rawFields[f.name] : (f.defaultValue !== undefined ? f.defaultValue : '');
          // Select fields: keep as string to match HTML select behavior (x-model always returns strings).
          // Number coercion happens at save time, not at load time.
          if (f.type === 'select') val = String(val);
          return { name: f.name, value: val, label: f.label, type: f.type, selectOptions: f.selectOptions || [], placeholder: f.placeholder || '' };
        });
      } else {
        // No schema match — fallback to guessing
        fields = Object.entries(rawFields).map(([k, v]) => ({
          name: k,
          value: v,
          label: k,
          type: this.guessFieldType(k, v),
          selectOptions: [],
        }));
      }
      // Seed the per-implementation field history so onSpecTypeChange can
      // restore the original loaded values when the user switches Type and
      // then back. _lastImpl tracks the implementation we'd be leaving on
      // the next change so the snapshot is filed under the correct key.
      const impl = arrSpec.implementation || '';
      const history = {};
      if (impl) {
        history[impl] = fields.map(f => ({ name: f.name, value: f.value, type: f.type }));
      }
      return {
        _key: ++this.cfEditorSpecCounter,
        name: arrSpec.name || '',
        implementation: impl,
        negate: arrSpec.negate || false,
        required: arrSpec.required || false,
        fields: fields,
        _lastImpl: impl,
        _fieldHistory: history,
      };
    },

    guessFieldType(name, value) {
      if (typeof value === 'boolean') return 'checkbox';
      if (typeof value === 'number') return 'number';
      if (name === 'value' && typeof value === 'string') return 'textbox';
      return 'textbox';
    },

    async loadCFEditorSchema() {
      const appType = this.cfEditorForm.appType;
      if (this.cfEditorSchema[appType]) return;

      this.cfEditorSchemaLoading = true;
      try {
        const res = await fetch(`/api/customformat/schema/${appType}`);
        if (res.ok) {
          const schema = await res.json();
          // Parse schema into usable format: [{implementation, implementationName, fields:[{name,label,type,selectOptions}]}]
          const parsed = (schema || []).map(s => ({
            implementation: s.implementation,
            implementationName: s.implementationName || s.implementation.replace('Specification', ''),
            fields: (s.fields || []).map(f => ({
              name: f.name,
              label: f.label || f.name,
              type: this.mapSchemaFieldType(f),
              selectOptions: (f.selectOptions || []).map(o => ({
                value: o.value !== undefined ? o.value : o.id,
                name: o.name || String(o.value ?? o.id),
              })),
              placeholder: f.helpText || '',
              defaultValue: f.value,
            })),
          }));
          this.cfEditorSchema = { ...this.cfEditorSchema, [appType]: parsed };
        }
      } catch (e) {
        console.error('Failed to load CF schema:', e);
      } finally {
        this.cfEditorSchemaLoading = false;
      }
    },

    mapSchemaFieldType(field) {
      if (field.type === 'textbox' || field.type === 'text') return 'textbox';
      if (field.type === 'number' || field.type === 'integer') return 'number';
      if (field.type === 'select' || field.type === 'selectOption' || (field.selectOptions && field.selectOptions.length > 0)) return 'select';
      if (field.type === 'checkbox' || field.type === 'bool') return 'checkbox';
      // Guess from name/value
      if (typeof field.value === 'boolean') return 'checkbox';
      if (typeof field.value === 'number') return 'number';
      return 'textbox';
    },

    getAvailableImplementations() {
      return this.cfEditorSchema[this.cfEditorForm.appType] || [];
    },

    populatePBCutoffSelect(el, qualityItems, selectedValue) {
      // Build options from items with allowed=true. When no items are allowed
      // the select has a single disabled "No allowed qualities" option. x-for
      // inside <select> doesn't re-render when items[].allowed toggles, hence
      // the programmatic approach.
      const allowed = (qualityItems || []).filter(q => q.allowed);
      el.innerHTML = '';
      if (allowed.length === 0) {
        const o = document.createElement('option');
        o.value = '';
        o.textContent = 'No allowed qualities';
        o.disabled = true;
        el.appendChild(o);
        return;
      }
      for (const item of allowed) {
        const o = document.createElement('option');
        o.value = item.name;
        o.textContent = item.name;
        el.appendChild(o);
      }
      // Preserve selection if still in allowed list; otherwise pick first.
      const stillValid = allowed.some(q => q.name === selectedValue);
      const targetValue = stillValid ? selectedValue : allowed[0].name;
      el.value = targetValue;
      // Programmatic assignment does NOT fire @change, so Alpine's
      // `pb.cutoff = $el.value` binding never runs when we auto-pick the
      // first allowed quality on a new profile. The dropdown looks selected
      // but pb.cutoff stays empty — export produces `cutoff: ""`. Dispatch
      // a change event so the binding runs. Safe from looping: x-effect's
      // next pass sees pb.cutoff == targetValue and skips the dispatch.
      if (targetValue !== selectedValue) {
        el.dispatchEvent(new Event('change', { bubbles: true }));
      }
    },

    populateCutoffSelect(el, qualityStructure, profile, selectedValue, qualityOverrides) {
      // Two sources depending on mode:
      // 1) STRUCTURE-DRIVEN: qualityStructure has entries — user has grouped or
      //    reordered via Edit Groups. Use allowed flag on each item.
      // 2) LEGACY FLAT-TOGGLE: qualityStructure is empty; user toggles write to
      //    qualityOverrides map keyed by name. Here we MUST apply the overrides
      //    on top of profile.items — otherwise a just-toggled-on resolution
      //    won't appear in the cutoff dropdown until user opens Edit Groups
      //    (which initializes qualityStructure). That was the v2.0.6 bug.
      let items;
      if (qualityStructure.length > 0) {
        items = qualityStructure.filter(i => i.allowed !== false);
      } else {
        const overrides = qualityOverrides || {};
        items = (profile?.items || []).filter(i => {
          const effective = overrides[i.name] !== undefined ? overrides[i.name] : i.allowed;
          return effective !== false;
        });
      }
      const trashDefault = profile?.cutoff || '';
      const trashValid = !trashDefault || items.some(i => i.name === trashDefault);
      const options = [];
      // TRaSH default option (first)
      if (trashDefault) {
        options.push({ value: trashDefault, name: trashDefault + (trashValid ? ' (TRaSH default)' : ' (TRaSH default — not in structure)'), disabled: !trashValid });
      }
      // All allowed items except TRaSH default (avoid duplicate)
      for (const item of items) {
        if (item.name !== trashDefault) options.push({ value: item.name, name: item.name });
      }
      // Skip option
      options.push({ value: '__skip__', name: '— Don\'t sync cutoff —' });
      // Rebuild options
      el.innerHTML = '';
      for (const opt of options) {
        const o = document.createElement('option');
        o.value = opt.value;
        o.textContent = opt.name;
        if (opt.disabled) o.disabled = true;
        el.appendChild(o);
      }
      const targetValue = selectedValue || trashDefault;
      if (el.value !== targetValue) el.value = targetValue;
      // Same class of bug populatePBCutoffSelect fixed: programmatic
      // el.value doesn't fire @change, so pdOverrides.cutoffQuality stays
      // at a stale value when the dropdown auto-corrects (e.g. user
      // toggles off the quality that was the cutoff, the list rebuilds,
      // el.value falls back to TRaSH default, but the override state
      // never updates). Dispatch so the @change binding runs.
      if (targetValue !== selectedValue) {
        el.dispatchEvent(new Event('change', { bubbles: true }));
      }
    },

    populateSelectField(el, options, selectedValue) {
      const currentCount = el.options.length;
      const needsRebuild = currentCount !== options.length;
      if (needsRebuild) {
        el.innerHTML = '';
        for (const opt of options) {
          const o = document.createElement('option');
          o.value = String(opt.value ?? opt);
          o.textContent = opt.name ?? String(opt.value ?? opt);
          el.appendChild(o);
        }
      }
      if (el.value !== selectedValue) el.value = selectedValue;
    },

    populateImplSelect(el, selectedImpl) {
      const impls = this.getAvailableImplementations();
      // Remove old dynamic options (keep first "Select type..." option)
      for (let i = el.options.length - 1; i > 0; i--) el.remove(i);
      // Add options from schema
      impls.forEach(impl => {
        const opt = document.createElement('option');
        opt.value = impl.implementation;
        opt.textContent = impl.implementationName || impl.implementation.replace('Specification', '');
        el.appendChild(opt);
      });
      el.value = selectedImpl;
    },

    // TRaSH trash_scores context keys, derived at runtime from the actual
    // CF JSON files on disk via /api/trash/{app}/score-contexts.
    // Keeps the Custom Format editor dropdown in sync with upstream TRaSH
    // (new SQP tiers, new language variants, etc.) without hardcoded lists.
    // Cached per appType in _trashScoreContextCache; lazy-loaded on first access.
    trashScoreContexts(appType) {
      if (!appType) return ['default'];
      const cached = this._trashScoreContextCache[appType];
      if (cached) return cached;
      // Seed with 'default' so the dropdown is never empty while the fetch
      // is in flight. Alpine will re-render once the cache is populated.
      if (this._trashScoreContextCache[appType] === undefined) {
        this._trashScoreContextCache[appType] = ['default'];
        fetch(`/api/trash/${appType}/score-contexts`)
          .then(r => r.ok ? r.json() : ['default'])
          .then(keys => {
            this._trashScoreContextCache = { ...this._trashScoreContextCache, [appType]: (keys && keys.length ? keys : ['default']) };
          })
          .catch(() => {});
      }
      return this._trashScoreContextCache[appType];
    },

    addCFSpec() {
      this.cfEditorForm.specifications.push({
        _key: ++this.cfEditorSpecCounter,
        name: '',
        implementation: '',
        negate: false,
        required: false,
        fields: [],
        _lastImpl: '',
        _fieldHistory: {},
      });
    },

    onSpecTypeChange(specIdx) {
      const spec = this.cfEditorForm.specifications[specIdx];
      const schema = this.getAvailableImplementations().find(s => s.implementation === spec.implementation);
      // Two-tier value preservation across Type changes so a fat-fingered
      // dropdown click doesn't silently destroy a typed regex:
      //
      //   1. Per-implementation memory: every time the user leaves an
      //      implementation, snapshot its fields into spec._fieldHistory
      //      keyed by the leaving implementation. Switching back later
      //      restores the snapshot — covers "I clicked the wrong type,
      //      went elsewhere, came back".
      //   2. Same-named compatible carry: when the new implementation has
      //      a field with the same name + type as the old one and the
      //      history doesn't have a snapshot for it, copy the current
      //      value forward. Covers "two regex-style specs sharing a 'value'
      //      textbox" (ReleaseTitle ↔ ReleaseGroup).
      //
      // The snapshot is taken from the PREVIOUSLY active implementation,
      // which we track via spec._lastImpl. spec._fieldHistory persists for
      // the editor's lifetime — populated either here or by openCFEditor's
      // initial seed of the spec's loaded values.
      spec._fieldHistory = spec._fieldHistory || {};
      const prevImpl = spec._lastImpl;
      if (prevImpl && prevImpl !== spec.implementation && Array.isArray(spec.fields)) {
        // Save outgoing field state under the implementation we're leaving.
        spec._fieldHistory[prevImpl] = spec.fields.map(f => ({
          name: f.name, value: f.value, type: f.type,
        }));
      }
      const oldFields = {};
      for (const f of (spec.fields || [])) {
        oldFields[f.name] = { value: f.value, type: f.type };
      }
      const remembered = spec._fieldHistory[spec.implementation] || null;
      const rememberedByName = {};
      if (remembered) {
        for (const f of remembered) rememberedByName[f.name] = f;
      }
      const resolveValue = (newName, newType, fallback) => {
        // Tier 1: prior visit to this implementation — restore exactly.
        const r = rememberedByName[newName];
        if (r && r.type === newType) return r.value;
        // Tier 2: carry from current fields when name + type match.
        const old = oldFields[newName];
        if (old && old.type === newType) return old.value;
        return fallback;
      };
      if (schema) {
        spec.fields = schema.fields.map(f => {
          const fallback = f.defaultValue !== undefined ? f.defaultValue : (f.type === 'checkbox' ? false : f.type === 'number' ? 0 : '');
          return {
            name: f.name,
            value: resolveValue(f.name, f.type, fallback),
            label: f.label,
            type: f.type,
            selectOptions: f.selectOptions || [],
            placeholder: f.placeholder || '',
          };
        });
      } else {
        spec.fields = [{ name: 'value', value: resolveValue('value', 'textbox', ''), label: 'Value', type: 'textbox', selectOptions: [], placeholder: '' }];
      }
      spec._lastImpl = spec.implementation;
    },

    getCFEditorPreviewJSON() {
      const f = this.cfEditorForm;
      const obj = {
        name: f.name,
        includeCustomFormatWhenRenaming: f.includeInRename,
        specifications: f.specifications.map(s => ({
          name: s.name,
          implementation: s.implementation,
          negate: s.negate,
          required: s.required,
          fields: s.fields.map(fld => ({ name: fld.name, value: fld.value })),
        })),
      };
      return JSON.stringify(obj, null, 2);
    },

    async saveCFEditor() {
      const f = this.cfEditorForm;
      if (!f.name.trim()) {
        this.cfEditorResult = { error: true, message: 'Name is required' };
        return;
      }
      if (f.specifications.length === 0) {
        this.cfEditorResult = { error: true, message: 'At least one specification is required' };
        return;
      }
      if (f.specifications.some(s => !s.implementation)) {
        this.cfEditorResult = { error: true, message: 'All specifications must have a type selected' };
        return;
      }
      // Whitespace-only or empty spec names slip past Arr's own length
      // checks but produce a "Condition name(s) cannot be empty or
      // consist of only spaces" 400 on first sync. Catch it here so the
      // user gets the feedback at save time.
      const blankSpecIdx = f.specifications.findIndex(s => !s.name || !s.name.trim());
      if (blankSpecIdx >= 0) {
        this.cfEditorResult = { error: true, message: `Specification #${blankSpecIdx + 1} needs a name (e.g. "Match WEB-DL").` };
        return;
      }

      const category = f.category === '' ? f.newCategory.trim() : f.category;
      if (!category) {
        this.cfEditorResult = { error: true, message: 'Please enter a category name' };
        return;
      }

      // Build payload in Arr field format: [{name, value}]
      // Coerce select field string values to numbers where appropriate (HTML select always returns strings)
      const specifications = f.specifications.map(s => ({
        name: s.name,
        implementation: s.implementation,
        negate: s.negate,
        required: s.required,
        fields: JSON.parse(JSON.stringify(s.fields.map(fld => {
          let val = fld.value;
          if (fld.type === 'select' && typeof val === 'string' && val !== '') {
            const n = Number(val);
            if (!isNaN(n)) val = n;
          }
          return { name: fld.name, value: val };
        }))),
      }));

      // Build trash_scores as object
      const trashScores = {};
      for (const ts of f.trashScores) {
        if (ts.context) trashScores[ts.context] = ts.score;
      }

      const payload = {
        name: f.name.trim(),
        appType: f.appType,
        category: category,
        includeInRename: f.includeInRename,
        specifications: specifications,
        trashId: f.trashId || '',
        trashScores: Object.keys(trashScores).length > 0 ? trashScores : undefined,
        description: f.description || '',
      };

      this.cfEditorSaving = true;
      this.cfEditorResult = null;

      try {
        let res;
        if (this.cfEditorMode === 'edit' && f.id) {
          // Update existing
          payload.id = f.id;
          res = await fetch(`/api/custom-cfs/${f.id}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
          });
        } else {
          // Create new
          res = await fetch('/api/custom-cfs', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ cfs: [payload] }),
          });
        }

        if (!res.ok) {
          let errMsg = 'Save failed';
          try { const err = await res.json(); errMsg = err.error || errMsg; } catch(_) {}
          this.cfEditorResult = { error: true, message: errMsg };
          // Re-enable the Save button so the user can adjust the name
          // and retry — the trailing reset below is unreachable after
          // this `return`, so reset locally.
          this.cfEditorSaving = false;
          return;
        }

        this.cfEditorResult = { error: false, message: this.cfEditorMode === 'edit' ? 'Updated successfully' : 'Created successfully' };
        // Refresh CF browse data
        this.loadCFBrowse(f.appType);
        // Close after brief delay to show success (keep saving state active)
        setTimeout(() => { this.showCFEditor = false; this.cfEditorSaving = false; }, 800);
        return; // skip finally's cfEditorSaving reset
      } catch (e) {
        this.cfEditorResult = { error: true, message: 'Network error: ' + e.message };
      }
      this.cfEditorSaving = false;
    },

    async deleteCustomCF(cf, appType) {
      if (!cf.isCustom || !cf.trashId) return;
      this.confirmModal = {
        show: true,
        title: 'Delete Custom Format',
        message: `Delete "${cf.name}"? This cannot be undone.`,
        confirmLabel: 'Delete',
        onConfirm: async () => {
          try {
            const res = await fetch(`/api/custom-cfs/${cf.trashId}`, { method: 'DELETE' });
            if (res.ok) {
              this.loadCFBrowse(appType);
            } else {
              let errMsg = 'Delete failed';
              try { const err = await res.json(); errMsg = err.error || errMsg; } catch(_) {}
              this.showToast(errMsg, 'error', 8000);
            }
          } catch (e) {
            this.showToast('Delete failed: ' + e.message, 'error', 8000);
          }
        },
        onCancel: null,
      };
    },

    async deleteCFFromEditor() {
      const f = this.cfEditorForm;
      if (!f.id) return;
      this.confirmModal = {
        show: true,
        title: 'Delete Custom Format',
        message: `Delete "${f.name}"? This cannot be undone.`,
        confirmLabel: 'Delete',
        onConfirm: async () => {
          try {
            const res = await fetch(`/api/custom-cfs/${f.id}`, { method: 'DELETE' });
            if (res.ok) {
              this.showCFEditor = false;
              this.loadCFBrowse(f.appType);
            } else {
              let errMsg = 'Delete failed';
              try { const err = await res.json(); errMsg = err.error || errMsg; } catch(_) {}
              this.cfEditorResult = { error: true, message: errMsg };
            }
          } catch (e) {
            this.cfEditorResult = { error: true, message: 'Delete failed: ' + e.message };
          }
        },
        onCancel: null,
      };
    },

    exportTrashJSON() {
      const f = this.cfEditorForm;
      const trashScores = {};
      for (const ts of f.trashScores) {
        if (ts.context) trashScores[ts.context] = ts.score;
      }

      const trashJSON = {
        trash_id: f.trashId || '',
        trash_scores: trashScores,
        name: f.name,
        includeCustomFormatWhenRenaming: f.includeInRename,
        specifications: f.specifications.map(s => ({
          name: s.name,
          implementation: s.implementation,
          negate: s.negate,
          required: s.required,
          fields: Object.fromEntries(s.fields.map(fld => [fld.name, fld.value])),
        })),
      };

      this.cfExportContent = JSON.stringify(trashJSON, null, 2);
      this.cfExportCopied = false;
    },

    // --- Import Custom CFs ---

    // Detect known cross-Arr CF spec incompatibilities. Returns an array of
    // issue objects for display. Only flags objectively-wrong cases or known
    // canonical-name mismatches — never custom-named CFs (we can't know
    // intent there). Empty result = import looks clean for target.
    _detectCrossArrImportIssues(cfs, targetApp) {
      // Spec implementations that exist in only one Arr — the other will
      // reject them at sync. Verified against TRaSH guide CF coverage.
      const ARR_ONLY_SPECS = {
        radarr: ['ReleaseTypeSpecification'],          // Sonarr-only (Single/Multi-episode/Season pack)
        sonarr: ['QualityModifierSpecification'],      // Radarr-only (Remux modifier)
      };
      // Source enum per Arr — values diverge between apps. Values verified
      // against TRaSH CF JSON conventions and Arr source code.
      const SOURCE_NAMES = {
        radarr: { 0:'Unknown', 1:'CAM', 2:'Telesync', 3:'Telecine', 4:'Workprint',
                  5:'DVD', 6:'TV', 7:'WEBDL', 8:'WEBRIP', 9:'Bluray' },
        sonarr: { 0:'Unknown', 1:'Television', 2:'TelevisionRaw', 3:'WEBDL',
                  4:'WEBRip', 5:'DVD', 6:'Bluray', 7:'BlurayRaw' },
      };
      // Known canonical Source names — only flag mismatch when spec.name
      // looks like one of these (TRaSH uses these). Unknown names = user
      // intent unclear, skip the check.
      const KNOWN_SOURCE = new Set(['webdl','webrip','bluray','blurayraw',
                                    'remux','blurayremux','dvd','television','tv',
                                    'cam','telesync','telecine','workprint','web']);
      // IndexerFlag — TRaSH only uses FreeLeech (1, same in both) and
      // Internal (Radarr=32, Sonarr=8). Cross-import value=32 to Sonarr is
      // out of range and silently broken.
      const KNOWN_INTERNAL_FLAG = { radarr: 32, sonarr: 8 };

      const normalize = s => (s || '').toLowerCase().replace(/^not\s+/i, '').replace(/[^a-z0-9]/g, '');
      const issues = [];

      for (const cf of cfs) {
        for (const spec of (cf.specifications || [])) {
          const impl = spec.implementation;

          // Check 1: spec types that only exist in the other app
          if ((ARR_ONLY_SPECS[targetApp] || []).includes(impl)) {
            issues.push({
              severity: 'error',
              cf: cf.name, spec: spec.name || '(unnamed)',
              message: `${impl} doesn't exist in ${targetApp} — will be rejected at sync`
            });
            continue;
          }

          const value = spec.fields?.value;
          if (value === undefined || value === null) continue;

          // Check 2: SourceSpecification — value out of range OR canonical-name mismatch
          if (impl === 'SourceSpecification') {
            const targetName = SOURCE_NAMES[targetApp]?.[value];
            if (!targetName) {
              issues.push({
                severity: 'error',
                cf: cf.name, spec: spec.name || '(unnamed)',
                message: `SourceSpecification value=${value} is out of range for ${targetApp}`
              });
            } else {
              const specNorm = normalize(spec.name);
              if (KNOWN_SOURCE.has(specNorm) && specNorm !== normalize(targetName)) {
                issues.push({
                  severity: 'warning',
                  cf: cf.name, spec: spec.name || '(unnamed)',
                  message: `Spec named "${spec.name}" with value=${value}, but in ${targetApp} value=${value} means "${targetName}"`
                });
              }
            }
          }

          // Check 3: IndexerFlagSpecification — Internal flag value mismatch
          if (impl === 'IndexerFlagSpecification') {
            const expectedInternal = KNOWN_INTERNAL_FLAG[targetApp];
            const sourceArr = targetApp === 'radarr' ? 'sonarr' : 'radarr';
            const sourceInternal = KNOWN_INTERNAL_FLAG[sourceArr];
            if (value === sourceInternal && value !== expectedInternal) {
              issues.push({
                severity: 'warning',
                cf: cf.name, spec: spec.name || '(unnamed)',
                message: `IndexerFlagSpecification value=${value} matches "Internal" in ${sourceArr} but means something else in ${targetApp} (Internal=${expectedInternal} there)`
              });
            }
          }
        }
      }
      return issues;
    },

    async _confirmCrossArrImport(issues, targetApp, cfCount) {
      const errors = issues.filter(i => i.severity === 'error');
      const warnings = issues.filter(i => i.severity === 'warning');
      let body = `Importing ${cfCount} CF(s) to ${targetApp}.\n\n`;
      if (errors.length) {
        body += 'ERRORS (these specs will not work in ' + targetApp + '):\n';
        for (const e of errors) body += `• [${e.cf}] ${e.spec}: ${e.message}\n`;
        body += '\n';
      }
      if (warnings.length) {
        body += 'LIKELY MISMATCHES (silent value misinterpretation):\n';
        for (const w of warnings) body += `• [${w.cf}] ${w.spec}: ${w.message}\n`;
        body += '\n';
      }
      body += `This JSON looks like it may be from a different Arr app. Source values use different enums between Radarr and Sonarr (e.g. value 7 means WEBDL in Radarr but BlurayRaw in Sonarr). Find a ${targetApp}-native version of this CF or edit the spec values after import.`;
      return new Promise(resolve => {
        this.confirmModal = {
          show: true,
          title: 'Cross-app compatibility check',
          message: body,
          confirmLabel: 'Import anyway',
          onConfirm: () => resolve(true),
          onCancel: () => resolve(false),
        };
      });
    },

    openImportCFModal(appType) {
      this.importCFAppType = appType;
      this.importCFSource = 'instance';
      this.importCFInstanceId = '';
      this.importCFList = [];
      this.importCFLoading = false;
      this.importCFCategory = 'Custom';
      this.importCFNewCategory = '';
      this.importCFJsonText = '';
      this.importCFJsonError = '';
      this.importCFResult = null;
      this.importCFImporting = false;
      this.showImportCFModal = true;
    },

    async fetchInstanceCFsForImport() {
      if (!this.importCFInstanceId) { this.importCFList = []; return; }
      this.importCFLoading = true;
      this.importCFList = [];
      try {
        // Fetch CFs from instance
        const res = await fetch(`/api/instances/${this.importCFInstanceId}/cfs`);
        const arrCFs = await res.json();
        // Fetch existing custom CFs to mark duplicates
        const existRes = await fetch(`/api/custom-cfs/${this.importCFAppType}`);
        const existing = await existRes.json();
        const existingNames = new Set((existing || []).map(c => c.name));
        // Also exclude TRaSH CFs (they're already in the browser)
        const trashRes = await fetch(`/api/trash/${this.importCFAppType}/cfs`);
        const trashCFs = await trashRes.json();
        const trashNames = new Set((trashCFs || []).map(c => c.name));

        // Don't filter TRaSH-name matches out — the user owns their
        // naming. Decorate them with a flag so the row can render an
        // informational badge instead. Save still works.
        this.importCFList = arrCFs
          .map(cf => ({
            name: cf.name,
            arrId: cf.id,
            specifications: cf.specifications,
            selected: false,
            exists: existingNames.has(cf.name),
            trashMatch: trashNames.has(cf.name),
          }))
          .sort((a, b) => a.name.localeCompare(b.name));
      } catch (e) {
        console.error('Failed to fetch CFs:', e);
      } finally {
        this.importCFLoading = false;
      }
    },

    async doImportCFs() {
      this.importCFResult = null;
      this.importCFJsonError = '';
      const category = this.importCFCategory === '' ? this.importCFNewCategory.trim() : this.importCFCategory;
      if (!category) {
        this.importCFResult = { error: true, message: 'Please enter a category name' };
        return;
      }

      this.importCFImporting = true;
      try {
        if (this.importCFSource === 'instance') {
          const selected = this.importCFList.filter(c => c.selected && !c.exists);
          if (selected.length === 0) {
            this.importCFResult = { error: true, message: 'No CFs selected' };
            return;
          }
          const res = await fetch('/api/custom-cfs/import-from-instance', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              instanceId: this.importCFInstanceId,
              cfNames: selected.map(c => c.name),
              category: category,
              appType: this.importCFAppType,
            }),
          });
          const result = await res.json();
          if (!res.ok) {
            this.importCFResult = { error: true, message: result.error || 'Import failed' };
            return;
          }
          // Only same-name-as-existing-custom collisions are skipped —
          // TRaSH-name matches are allowed through (user owns naming).
          const customSkipped = (result.skippedCollisions || []).length;
          const suffix = customSkipped > 0
            ? ` (${customSkipped} skipped — same name as existing custom CF)`
            : '';
          this.importCFResult = { error: false, message: `Imported ${result.added} CF(s)${suffix}` };
          // Mark imported CFs as existing
          for (const cf of this.importCFList) {
            if (cf.selected) cf.exists = true;
          }
        } else {
          // JSON import
          let parsed;
          try {
            parsed = JSON.parse(this.importCFJsonText);
          } catch (e) {
            this.importCFJsonError = 'Invalid JSON: ' + e.message;
            return;
          }
          // Accept both single CF and array
          if (!Array.isArray(parsed)) parsed = [parsed];
          const cfs = parsed.map(cf => ({
            name: cf.name || 'Unnamed CF',
            appType: this.importCFAppType,
            category: category,
            // Honor includeCustomFormatWhenRenaming from imported JSON. The Arr
            // API uses the long key on the CF; clonarr stores it as
            // includeInRename internally. Without this map, importing a TRaSH
            // JSON like pcok.json (which has the flag set true) silently
            // landed it as false in the editor.
            includeInRename: !!cf.includeCustomFormatWhenRenaming,
            specifications: cf.specifications || [],
          }));

          // Cross-Arr compatibility check. Radarr and Sonarr share most spec
          // types (ReleaseTitle, ReleaseGroup, Resolution, Language) but
          // diverge on a few value-encoded ones, so importing a Radarr JSON
          // to Sonarr (or vice-versa) silently misinterprets the value field.
          // Most reported case: SourceSpec WEBDL=7 in Radarr → 7 means
          // BlurayRaw in Sonarr.
          const issues = this._detectCrossArrImportIssues(cfs, this.importCFAppType);
          if (issues.length > 0) {
            const ok = await this._confirmCrossArrImport(issues, this.importCFAppType, cfs.length);
            if (!ok) {
              this.importCFImporting = false;
              return;
            }
          }

          const res = await fetch('/api/custom-cfs', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ cfs }),
          });
          const result = await res.json();
          if (!res.ok) {
            this.importCFResult = { error: true, message: result.error || 'Import failed' };
            return;
          }
          this.importCFResult = { error: false, message: `Imported ${result.added} CF(s)` };
        }
        // Refresh CF browse data
        this.loadCFBrowse(this.importCFAppType);
      } catch (e) {
        this.importCFResult = { error: true, message: 'Error: ' + e.message };
      } finally {
        this.importCFImporting = false;
      }
    },

  },
};

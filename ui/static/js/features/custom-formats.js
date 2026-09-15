import { sanitizeHTML } from '../utils/csrf.js';

export default {
  state: {
    // Custom Formats browse - name/category text filter. Single string
    // applies across all categories simultaneously; matching categories
    // auto-expand so results are visible without manual clicks.
    cfBrowseFilter: '',
    // Browse list view mode. 'description' (default) shows the CF
    // description inline + lets the user click a row's name to expand
    // an inline conditions panel below it. 'conditions' shows the
    // condition pills inline directly (no per-row expand needed since
    // they are already in view). Persisted to localStorage.
    cfBrowseViewMode: localStorage.getItem('clonarr_cfBrowseViewMode') || 'description',
    // Single-open per-row conditions panel. Only meaningful in
    // 'description' view mode. Empty = no row expanded.
    cfBrowseExpandedCF: '',
    // Sidebar category-filter selection. Three formats:
    //   'all'                 - every category (today's stacked cards)
    //   'parent:<prefix>'     - filter to every sub-group under a parent
    //                           (e.g. 'parent:Unwanted' = all Unwanted variants)
    //   '<displayName>'       - filter to one specific sub-group
    // Persisted to localStorage so the choice survives reload.
    cfBrowseActiveCategory: localStorage.getItem('clonarr_cfBrowseCategory') || 'all',
    // Per-parent expand state for the sidebar tree. Independent of
    // main-pane detailSections - sidebar parents collapse to a single
    // row while main cards stay individually controllable. Stored as
    // {<parentPrefix>: bool}. Persisted so user's "what's interesting
    // to me" survives reload.
    cfBrowseSidebarExpanded: (() => {
      try { return JSON.parse(localStorage.getItem('clonarr_cfBrowseExpanded') || '{}'); }
      catch (_) { return {}; }
    })(),
    // Clone-flow modal state - set by cloneCFRow when the ⧉ button is
    // clicked, cleared by cancelCloneCF / commitCloneCF (backdrop
    // click is NOT a close path - Cancel button or ESC only, matches
    // the modal-no-backdrop-close rule). No API call happens until
    // the user clicks Save in the modal.
    cloneModal: { open: false, sourceCF: null, sourceAppType: '', name: '', saving: false, error: '' },
    // Add-to-Arr modal state - set by openAddCFToArr when the +Arr
    // button is clicked, cleared by cancelAddCFToArr / commitAddCFToArr.
    // Pushes the CF entity to a chosen Arr instance without touching
    // any quality profile. Same modal-no-backdrop-close convention.
    // Add-CF-to-Arr modal - Custom Formats Browse + Add flow.
    // target='arr'     : push the CF entity to the chosen instance only;
    //                    no profile mutation, current "+" behaviour.
    // target='profile' : add to a sync rule's profile with a score.
    //                    ruleId picks the rule (filtered to profiles
    //                    that do not already manage this CF); score is
    //                    pre-filled from the CF's trash_scores entry
    //                    for the rule's profile context.
    addCFToArrModal: { open: false, cfName: '', trashId: '', customCFId: '', appType: '', instanceId: '', target: 'arr', ruleId: '', score: 0, saving: false, error: '' },
    // cfEditorActiveTab + cfEditorDescriptionPreview live in state.js
    // alongside the rest of the cf-editor state - this section only
    // holds CF browse / clone state.
  },
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
        // Invalidate the /all-cfs catalog cache so Sync Preview's
        // Additional CF picker + Diffs view pick up freshly created /
        // edited / deleted custom CFs immediately. loadCFBrowse is the
        // central re-fetch point hit by save / delete / import flows.
        // Without this, the cache marker stays set and the picker
        // serves stale catalog until container restart.
        if (this._extraCFGroupsCachedType === appType) {
          this._extraCFGroupsCachedType = null;
          this.extraCFGroups = [];
        }
      } catch (e) { /* not yet cloned */ }
    },

    async loadConflicts(appType) {
      try {
        const res = await fetch(`/api/trash/${appType}/conflicts`);
        if (res.ok) this.conflictsData = { ...this.conflictsData, [appType]: await res.json() };
      } catch (e) { /* not available */ }
    },

    // Filtered view of getCFBrowseGroups. When cfBrowseFilter is empty,
    // returns the raw category list. When set:
    //   - Category whose displayName matches → include with all CFs intact
    //   - Otherwise, filter each group's CFs by name match; drop groups
    //     with no matches; drop the category if all groups are empty.
    // Case-insensitive substring match. The result feeds the template so
    // categories with zero matching CFs disappear entirely.
    filteredCFBrowseGroups(appType) {
      const filter = (this.cfBrowseFilter || '').trim().toLowerCase();
      let groups = this.getCFBrowseGroups(appType) || [];
      // Sidebar category pin only applies when search is empty. When
      // the user types a query, broaden across every category so a
      // CF can be found without first remembering which sidebar
      // entry it lives under. The row's source group is rendered in
      // the result, so cross-category surfacing does not confuse.
      if (!filter) {
        const active = this.cfBrowseActiveCategory;
        if (active && active !== 'all') {
          if (active.startsWith('parent:')) {
            const parentName = active.slice('parent:'.length);
            groups = groups.filter(cat => (cat.category || 'Other') === parentName);
          } else {
            groups = groups.filter(cat => cat.displayName === active);
          }
        }
        return groups;
      }
      // Multi-term OR-match. Splits on whitespace so a query like
      // "5.1 7.1" surfaces both 5.1 Surround AND 7.1 Surround. Each
      // term tests against name, description, AND specification
      // labels (the condition pills the user already sees on each
      // row), so "atmos" finds CFs whose conditions mention Atmos
      // even when the name does not.
      const terms = filter.split(/\s+/).filter(t => t.length > 0);
      const matchCF = (cf) => {
        const haystacks = [
          (cf.name || '').toLowerCase(),
          (cf.description || '').toLowerCase(),
        ];
        // Only positive specs contribute to the haystack. TRaSH audio
        // CFs typically carry a stack of "Not <other format>" negated
        // specs (PCM has "Not TrueHD/ATMOS" so it stays mutually
        // exclusive). Without the negate-skip, searching "atmos" would
        // match every audio CF via its NOT-Atmos spec - the opposite
        // of intent. spec.implementation is dropped from the haystack
        // because it's the same boilerplate name across the catalog
        // ("ReleaseTitleSpecification") and never carries user signal.
        for (const s of (cf.specifications || [])) {
          if (!s || s.negate) continue;
          if (s.name) haystacks.push(String(s.name).toLowerCase());
        }
        // Make the rename indicator searchable: a CF flagged to append its
        // name to renamed files matches the term "rename".
        if (cf.includeInRename) haystacks.push('rename');
        return terms.some(term => haystacks.some(hay => hay.includes(term)));
      };
      return groups
        .map(cat => {
          const filteredGroups = (cat.groups || [])
            .map(g => ({ ...g, cfs: (g.cfs || []).filter(matchCF) }))
            .filter(g => g.cfs.length > 0);
          if (filteredGroups.length === 0) return null;
          return {
            ...cat,
            groups: filteredGroups,
            totalCFs: filteredGroups.reduce((acc, g) => acc + g.cfs.length, 0),
          };
        })
        .filter(Boolean);
    },

    // TRaSH name → kebab-case slug used in both the docs anchors and
    // the GitHub JSON filenames. "1.0 Mono" → "10-mono", "5.1 Surround"
    // → "51-surround", "BR-DISK" → "br-disk". Rules: lowercase, drop
    // dots, every non-alphanumeric run collapses to one dash, trim
    // leading/trailing dashes.
    _cfSlug(name) {
      // Fallback only (prefer the backend jsonSlug). Drop "." and "/" WITHOUT a
      // separator so TRaSH's filename convention is matched: "DV (w/o HDR
      // fallback)" -> "dv-wo-hdr-fallback", "x265 (no HDR/DV)" -> "x265-no-hdrdv".
      // Everything else non-alphanumeric collapses to a single "-".
      return (name || '').toLowerCase()
        .replace(/[./]/g, '')
        .replace(/[^a-z0-9]+/g, '-')
        .replace(/^-+|-+$/g, '');
    },

    // Links a CF to the TRaSH-Guides docs collection page with an
    // anchor that scrolls to the CF section. Falls back to the
    // collection landing page if the slug can't be derived. Empty for
    // custom CFs (no upstream).
    //
    // TRaSH-Guides uses inconsistent casing per app - Radarr's page
    // path is /Radarr/Radarr-collection-of-custom-formats/ (both
    // capitalized) but Sonarr's is /Sonarr/sonarr-collection-of-custom-
    // formats/ (capital dir, lowercase filename). Verified by curl
    // against trash-guides.info - the wrong case returns 404. The
    // ?h={slug} query param triggers Material's search-highlight on
    // the destination page.
    trashCFGuideUrl(cf, appType) {
      if (!cf || cf.isCustom) return '';
      // Prefer backend-provided JSONSlug (the actual disk filename
      // stem) so language CFs like name="VFQ" / file="french-vfq.json"
      // resolve to the right anchor. Fall back to the slug-from-name
      // guess for CFs loaded from older container builds that did not
      // populate JSONSlug yet.
      const slug = cf.jsonSlug || this._cfSlug(cf.name);
      const base = appType === 'sonarr'
        ? 'https://trash-guides.info/Sonarr/sonarr-collection-of-custom-formats/'
        : 'https://trash-guides.info/Radarr/Radarr-collection-of-custom-formats/';
      return slug ? `${base}?h=${slug}#${slug}` : base;
    },

    // Links a CF to the raw JSON file on TRaSH-Guides GitHub. TRaSH
    // names files by a kebab-case slug that does NOT always match the
    // CF's name field - language CFs in particular get a language
    // prefix (vfq.json -> french-vfq.json). Use the backend-provided
    // JSONSlug when available; fall back to deriving from name.
    trashCFJsonUrl(cf, appType) {
      if (!cf || cf.isCustom) return '';
      const slug = cf.jsonSlug || this._cfSlug(cf.name);
      if (!slug) return '';
      const app = (appType === 'sonarr' || appType === 'radarr') ? appType : 'radarr';
      return `https://github.com/TRaSH-Guides/Guides/blob/master/docs/json/${app}/cf/${slug}.json`;
    },

    // Look up a full CF object by trash_id, walking both the TRaSH catalog
    // and the user's custom CFs cached in cfBrowseData. Used by the Custom
    // Formats > In use sub-tab to wire the same hover-tooltip as Profile
    // Sync (showCFTooltip + buildCFInfoHTML), since the cf-sync-rules API
    // response only carries trashId + name and not the description.
    findCFByTrashId(appType, trashId) {
      if (!trashId) return null;
      const data = this.cfBrowseData?.[appType];
      if (!data) return null;
      for (const cf of (data.cfs || [])) {
        if (cf.trash_id === trashId) {
          return { ...cf, trashId: cf.trash_id, isCustom: false };
        }
      }
      for (const cf of (data.customCFs || [])) {
        if (cf.id === trashId || cf.trash_id === trashId) {
          return { ...cf, trashId: cf.id || cf.trash_id, isCustom: true };
        }
      }
      return null;
    },

    // Row-tooltip glue for the In use sub-tab. Resolves the full CF on
    // hover and short-circuits if there's nothing worth showing (no
    // description and not a TRaSH CF with guide/json links). Keeps
    // findCFByTrashId out of the Alpine render path - we only walk
    // cfBrowseData when the user actually hovers a row.
    cfRowShowTooltip(event, trashId) {
      const cf = this.findCFByTrashId(this.activeAppType, trashId);
      if (!cf) return;
      const hasLinks = !cf.isCustom &&
        ((this.trashCFGuideUrl ? this.trashCFGuideUrl(cf, this.activeAppType) : '') ||
         (this.trashCFJsonUrl ? this.trashCFJsonUrl(cf, this.activeAppType) : ''));
      if (!cf.description && !hasLinks) return;
      this.showCFTooltip(event, this.buildCFInfoHTML(cf, this.activeAppType));
    },

    // Category-snippet helper for the collapsed-card preview line.
    // Strips TRaSH's HTML description down to plain text and truncates
    // to ~120 chars. Memoised on the cat object itself (a hidden
    // `_snippetCache` key) so Alpine re-evaluating this every
    // keystroke / filter change / expand toggle doesn't re-parse the
    // HTML through document.createElement each call. The cat objects
    // are rebuilt on every catalog refresh, so the cache invalidates
    // naturally with the data.
    cfCategorySnippet(cat) {
      if (!cat) return '';
      if (cat._snippetCache !== undefined) return cat._snippetCache;
      const html = cat.trashDescription;
      if (!html) { cat._snippetCache = ''; return ''; }
      const tmp = document.createElement('div');
      tmp.innerHTML = html;
      const text = (tmp.textContent || tmp.innerText || '').replace(/\s+/g, ' ').trim();
      if (!text) { cat._snippetCache = ''; return ''; }
      cat._snippetCache = text.length > 120 ? text.slice(0, 120).trim() + '…' : text;
      return cat._snippetCache;
    },

    // Set the sidebar category filter and persist the choice.
    // CLEARS any explicit chevron-set state on the relevant parent so
    // the auto-expand-via-active branch in isCFBrowseParentExpanded
    // takes over. Effect: clicking a label re-opens that parent even
    // if it was manually collapsed, and other parents auto-close
    // naturally (their active-match check goes false once active
    // moves to a different parent). Matches the profile editor's
    // pattern where label clicks clear the chevron's explicit
    // override on the same section.
    setCFBrowseCategory(name) {
      this.cfBrowseActiveCategory = name || 'all';
      try { localStorage.setItem('clonarr_cfBrowseCategory', this.cfBrowseActiveCategory); } catch (_) {}

      // Figure out which sidebar parent (if any) the new active filter
      // belongs to - either directly (parent:X) or via a child's parent.
      let targetParent = null;
      if (this.cfBrowseActiveCategory.startsWith('parent:')) {
        targetParent = this.cfBrowseActiveCategory.slice('parent:'.length);
      } else if (this.cfBrowseActiveCategory !== 'all') {
        const tree = this.cfBrowseCategoriesHierarchy(this.activeAppType) || [];
        for (const par of tree) {
          if ((par.children || []).some(c => c.displayName === this.cfBrowseActiveCategory)) {
            targetParent = par.parent;
            break;
          }
        }
      }
      // Clear the explicit override on the target parent - auto-expand
      // (via the active-match check) takes over from here.
      if (targetParent && Object.prototype.hasOwnProperty.call(this.cfBrowseSidebarExpanded, targetParent)) {
        const updated = { ...this.cfBrowseSidebarExpanded };
        delete updated[targetParent];
        this.cfBrowseSidebarExpanded = updated;
        try { localStorage.setItem('clonarr_cfBrowseExpanded', JSON.stringify(updated)); } catch (_) {}
      }

      // For single-child pins, also force the matching main-pane card open.
      if (this.cfBrowseActiveCategory !== 'all'
          && !this.cfBrowseActiveCategory.startsWith('parent:')) {
        this.detailSections = {
          ...this.detailSections,
          ['cfb_' + this.cfBrowseActiveCategory]: true,
        };
      }
    },

    // Toggle a single parent's expansion in the sidebar tree.
    // Sets explicit to the inverse of what's CURRENTLY VISIBLE - not
    // the inverse of the stored explicit flag. The difference matters
    // when the parent is auto-expanded (via active-filter match) with
    // no explicit value set: a plain "!stored" toggle would flip
    // undefined → true (no visible change on first click). Inverting
    // the visible state means the first chevron click always does
    // what the user expects (collapse a visibly-open parent).
    toggleCFBrowseParent(parent, children) {
      const currentlyVisible = this.isCFBrowseParentExpanded(parent, children);
      this.cfBrowseSidebarExpanded = {
        ...this.cfBrowseSidebarExpanded,
        [parent]: !currentlyVisible,
      };
      try { localStorage.setItem('clonarr_cfBrowseExpanded', JSON.stringify(this.cfBrowseSidebarExpanded)); } catch (_) {}
    },

    // True when a parent is currently expanded. Explicit chevron-set
    // state wins (true OR false - so the user can collapse a parent
    // that's also the active filter target). When the user has never
    // touched the chevron on this parent (state is undefined), fall
    // back to auto-expand: open if the parent or any of its children
    // is the active filter target so the highlighted entry is always
    // visible. setCFBrowseCategory re-sets the explicit flag to true
    // when the user re-clicks the parent or one of its children, so
    // re-pinning after manual collapse re-expands.
    isCFBrowseParentExpanded(parent, children) {
      const explicit = this.cfBrowseSidebarExpanded[parent];
      if (explicit !== undefined) return explicit;
      const active = this.cfBrowseActiveCategory;
      if (active === 'parent:' + parent) return true;
      if (active && active !== 'all' && !active.startsWith('parent:')) {
        return (children || []).some(c => c.displayName === active);
      }
      return false;
    },

    // Group the flat category list by their bracket-prefix into a
    // two-level structure: parents (Audio, HDR, Unwanted, ...) with
    // children (Audio Formats, Audio Channels, Unwanted Default,
    // Unwanted SQP, ...). Each parent carries its total CF count
    // across all children. Custom user-created CFs collapse into
    // a "Custom" parent on top.
    cfBrowseCategoriesHierarchy(appType) {
      const flat = this.getCFBrowseGroups(appType) || [];
      const byParent = new Map();
      for (const cat of flat) {
        const key = cat.category || 'Other';
        if (!byParent.has(key)) byParent.set(key, []);
        byParent.get(key).push(cat);
      }
      const out = [];
      for (const [parent, children] of byParent) {
        out.push({
          parent,
          children,
          totalCFs: children.reduce((a, c) => a + (c.totalCFs || 0), 0),
          isCustom: children.some(c => c.isCustom),
        });
      }
      return out;
    },

    // True when the category should render expanded. Search results
    // need to be visible without manual clicks (cfBrowseFilter branch);
    // otherwise the explicit detailSections flag wins. setCFBrowseCategory
    // sets that flag to true when the sidebar pins a single child
    // category, so card-auto-expansion-on-sidebar-click still works -
    // but the chevron and "Collapse all" can override it because they
    // also write to detailSections.
    isCFCategoryExpanded(cat) {
      return !!this.cfBrowseFilter || !!this.detailSections['cfb_' + cat.displayName];
    },

    // True if ANY sidebar parent or main category card is currently
    // expanded. Drives the Expand all / Collapse all toggle label so
    // the button always offers the action that has effect.
    anyCFCategoryExpanded(appType) {
      const cats = this.getCFBrowseGroups(appType) || [];
      if (cats.some(c => !!this.detailSections['cfb_' + c.displayName])) return true;
      const tree = this.cfBrowseCategoriesHierarchy(appType) || [];
      return tree.some(par => !!this.cfBrowseSidebarExpanded[par.parent]);
    },

    // Flip every sidebar parent AND every category card open or closed
    // in one go. If any is open → collapse all. Otherwise expand all.
    // Single toggle covers both views so the user doesn't need to
    // know which "expand" they're after.
    toggleAllCFCategories(appType) {
      const next = !this.anyCFCategoryExpanded(appType);
      // Sidebar parents
      const tree = this.cfBrowseCategoriesHierarchy(appType) || [];
      const sideUpdate = { ...this.cfBrowseSidebarExpanded };
      for (const par of tree) {
        sideUpdate[par.parent] = next;
      }
      this.cfBrowseSidebarExpanded = sideUpdate;
      try { localStorage.setItem('clonarr_cfBrowseExpanded', JSON.stringify(sideUpdate)); } catch (_) {}
      // Main cards
      const cats = this.getCFBrowseGroups(appType) || [];
      const cardUpdate = { ...this.detailSections };
      for (const c of cats) {
        cardUpdate['cfb_' + c.displayName] = next;
      }
      this.detailSections = cardUpdate;
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
        // No prefix remapping - bracket prefix from TRaSH cf-group JSON is
        // the source of truth (mirrors backend's ParseCategoryPrefix). Earlier
        // `Required → Golden Rule` and `SQP → Miscellaneous` remaps were
        // removed: they pre-empted TRaSH's classification choices and broke
        // when TRaSH started using prefixes for new purposes (e.g.
        // `[Required] Repack/Proper`, `[Required] Anime Versions` are not
        // Golden Rule groups).
        // Display name: use shortName if present, otherwise prefix, otherwise full name
        const displayName = shortName ? (prefix + ' - ' + shortName) : (prefix || group.name);
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
            jsonSlug: cf?.jsonSlug || '',
            score: cf?.trash_scores?.default,
            specifications: cf?.specifications || [],
            // Surfaced to the Browse row as a small "rename" pill so
            // users can tell which CFs append their name to renamed
            // files. Backend serialises as includeCustomFormatWhenRenaming.
            includeInRename: !!cf?.includeCustomFormatWhenRenaming,
            category: categoryClass,
          });
        }

        if (cfs.length > 0) {
          categories.push({
            category: categoryClass,
            displayName,
            // Short name (no prefix) - used in sidebar children where
            // the parent header already conveys the prefix, so we don't
            // want to repeat it. Falls back to displayName when there's
            // no bracket-prefix (e.g. "Other" category).
            shortName: shortName || displayName,
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
          ungrouped.push({
            trashId: cf.trash_id,
            name: cf.name,
            description: cf.description || '',
            score: cf.trash_scores?.default,
            specifications: cf.specifications || [],
            category: 'Other',
          });
        }
      }
      if (ungrouped.length > 0) {
        ungrouped.sort((a, b) => a.name.localeCompare(b.name));
        categories.push({ category: 'Other', displayName: 'Other', groupNum: null, isCustom: false, groups: [{ name: 'Other', shortName: 'Other', cfs: ungrouped }], totalCFs: ungrouped.length });
      }

      // Inject custom CFs - grouped by their user-chosen category
      // field. Each unique category becomes its own card on the page;
      // they all nest under a "Custom" sidebar parent so user-defined
      // buckets don't pollute the TRaSH category tree.
      const customCFs = data.customCFs || [];
      if (customCFs.length > 0) {
        const byCategory = new Map();
        for (const ccf of customCFs) {
          const cat = (ccf.category || '').trim() || 'Custom';
          if (!byCategory.has(cat)) byCategory.set(cat, []);
          byCategory.get(cat).push({
            trashId: ccf.id,
            name: ccf.name,
            description: ccf.description || '',
            score: ccf.trashScores?.default,
            specifications: ccf.specifications || [],
            // Surfaced as a "rename" pill on the row when true.
            includeInRename: !!ccf.includeInRename,
            // Pill-color tinting reads this for the row background
            // hint. User-defined cats don't have an --cat-* token, so
            // pills fall back to neutral; the orange "is-custom" badge
            // on the row still signals custom-ness.
            category: 'Custom',
            isCustom: true,
            rawCustom: ccf,
          });
        }
        // Sort category names alphabetically with the default "Custom"
        // pinned at the top so a brand-new user without any explicit
        // categories sees a familiar starting point.
        const catNames = [...byCategory.keys()].sort((a, b) => {
          if (a === 'Custom') return -1;
          if (b === 'Custom') return 1;
          return a.localeCompare(b);
        });
        for (const catName of catNames) {
          const cfs = byCategory.get(catName);
          cfs.sort((a, b) => a.name.localeCompare(b.name));
          categories.push({
            // `category` is the SIDEBAR PARENT key - always 'Custom'
            // so every user-category collapses under one Custom parent
            // in the sidebar tree (see cfBrowseCategoriesHierarchy).
            category: 'Custom',
            displayName: catName,
            shortName: catName,
            groupNum: null,
            isCustom: true,
            groups: [{ name: catName, shortName: catName, cfs }],
            totalCFs: cfs.length,
          });
        }
      }

      // Group-integer sort (see _compareCFGroups): cf-groups with explicit
      // `group` field sort first by integer, then "Other" tier, then "Custom"
      // tier last. Display-name alphabetical tiebreak within tiers.
      return categories.sort((a, b) =>
        this._compareCFGroups(a.displayName, a.groupNum, !!a.isCustom,
                              b.displayName, b.groupNum, !!b.isCustom));
    },

    // --- Row-level helpers (Profilarr-inspired condition pills + clone) ---

    // Map Arr "<X>Specification" implementation names to compact labels
    // the user can scan at a glance. Anything unmapped falls through as
    // the raw type stripped of the "Specification" suffix.
    _cfSpecShortName(impl) {
      const m = {
        ReleaseTitleSpecification: 'Title',
        LanguageSpecification: 'Language',
        SourceSpecification: 'Source',
        ResolutionSpecification: 'Resolution',
        SizeSpecification: 'Size',
        ReleaseGroupSpecification: 'Group',
        IndexerFlagSpecification: 'Flag',
        QualityModifierSpecification: 'Modifier',
        ReleaseTypeSpecification: 'Type',
        EditionSpecification: 'Edition',
      };
      if (m[impl]) return m[impl];
      return (impl || '').replace(/Specification$/, '') || 'Condition';
    },

    // Pull a human-readable value out of an Arr/TRaSH spec's `fields`
    // payload. Fields is either a JSON-encoded array/object (from TRaSH
    // CFSpecification.Fields json.RawMessage) or an already-parsed array
    // (from custom CFs stored in clonarr.json). We normalise both and
    // pick the first scalar `value` we find.
    _cfSpecValue(spec) {
      let fields = spec?.fields;
      if (typeof fields === 'string') {
        try { fields = JSON.parse(fields); } catch (_) { return ''; }
      }
      if (!fields) return '';
      const arr = Array.isArray(fields) ? fields : [fields];
      for (const f of arr) {
        if (f && f.value !== undefined && f.value !== null) {
          if (typeof f.value === 'object') {
            // Resolution / Source / Language specs sometimes carry an
            // object like { name: "...", value: <int> }. Surface name.
            if (f.value.name) return String(f.value.name);
            try { return JSON.stringify(f.value); } catch (_) { return ''; }
          }
          return String(f.value);
        }
      }
      return '';
    },

    // Build the visible pill list for a CF row. Uses spec.name as the
    // pill label - TRaSH (and the Arr UIs) already give every spec a
    // human-readable name like "Mono", "Not 3.0ch", "1080p", or
    // "Bluray", which is far more meaningful than regex or impl-type.
    // For structured specs (Resolution / Source / Language) the value
    // text gets appended when the name is generic enough to need it.
    //
    // Returns up to `max` pills + an "+N more" overflow chip; full
    // regex / value breakdown is always available via the info popover.
    cfConditionPills(cf, max) {
      const specs = cf?.specifications || [];
      if (!specs.length) return [];
      const limit = Math.max(1, max || 3);

      const pills = [];
      for (const s of specs) {
        const rawName = (s.name || '').trim();
        const implLabel = this._cfSpecShortName(s.implementation);
        const value = this._cfSpecValue(s);
        const isStructured = !this._cfRegexImpls.has(s.implementation);

        // Decide what reads best on the pill. Order of preference:
        //  1. spec.name (the curated label TRaSH gives every spec)
        //  2. structured value (Resolution: 1080p)
        //  3. implementation fallback (Title)
        let label, valueText = '';
        if (rawName) {
          label = rawName;
          if (isStructured && value && !rawName.toLowerCase().includes(value.toLowerCase())) {
            valueText = value;
          }
        } else if (isStructured && value) {
          label = implLabel;
          valueText = value;
        } else {
          label = implLabel;
        }

        pills.push({
          label,
          value: valueText,
          required: !!s.required,
          negate: !!s.negate,
          full: label + (valueText ? ': ' + valueText : '') + (s.required ? ' (required)' : '') + (s.negate ? ' - exclude' : ''),
        });
      }

      if (pills.length <= limit) return pills;
      const shown = pills.slice(0, limit);
      shown.push({ overflow: true, count: pills.length - limit, label: `+${pills.length - limit}`, full: `${pills.length - limit} more conditions` });
      return shown;
    },

    // Implementations whose `value` is a free-form regex pattern.
    // Used by cfConditionPills to decide which specs get a
    // structured pill value vs a name-only pill.
    _cfRegexImpls: new Set([
      'ReleaseTitleSpecification',
      'ReleaseGroupSpecification',
      'EditionSpecification',
    ]),

    // Hover-tooltip for the +N overflow chip - shows the aggregated
    // pill summary (same form the row would render if there was no
    // limit) instead of the raw regex. Full regex detail is in the
    // info popover for users who actually need it.
    cfAllConditionsTooltip(cf) {
      const pills = this.cfConditionPills(cf, 999) || [];
      return pills.filter(p => !p.overflow).map(p => p.full).join('\n');
    },


    // Read/write helper for the editor's "default score" field on the
    // General tab. The underlying storage is the trashScores array
    // (one row per context). The General-tab field exposes only the
    // "default" context so casual users don't have to touch the
    // TRaSH-tab; power users can still set per-context scores there.
    get cfEditorDefaultScore() {
      const arr = this.cfEditorForm?.trashScores || [];
      const def = arr.find(t => (t.context || 'default') === 'default');
      return def ? (def.score ?? 0) : 0;
    },
    set cfEditorDefaultScore(val) {
      const n = Number(val);
      const score = isNaN(n) ? 0 : n;
      const arr = this.cfEditorForm.trashScores || [];
      const idx = arr.findIndex(t => (t.context || 'default') === 'default');
      if (idx >= 0) {
        arr[idx] = { ...arr[idx], score };
      } else {
        arr.push({ _key: ++this.cfEditorScoreCounter, context: 'default', score });
      }
      this.cfEditorForm = { ...this.cfEditorForm, trashScores: arr };
    },

    // Collect every category currently used by custom CFs for this
    // app, merged with the preset starter list. Drives the editor's
    // Category dropdown so once a user creates "My Audio Tweaks", it
    // shows up in the picker the next time they create a CF.
    cfEditorCategoryOptions() {
      const preset = ['Custom', 'Audio', 'HDR Formats', 'HQ Release Groups', 'Resolution', 'Streaming Services', 'Miscellaneous', 'Optional', 'Unwanted', 'Movie Versions', 'Anime', 'Language Profiles'];
      const appType = this.cfEditorForm?.appType;
      const userCats = new Set();
      const customs = this.cfBrowseData?.[appType]?.customCFs || [];
      for (const ccf of customs) {
        const c = (ccf.category || '').trim();
        if (c) userCats.add(c);
      }
      // Always include the current form's category so the dropdown
      // can select it on edit, even if the catalog hasn't loaded yet
      // OR the user is editing the only CF in that category (catalog
      // would still surface it, but be defensive - race conditions
      // around openCFEditor showed the dropdown falling back to
      // "New category" without this).
      const formCat = (this.cfEditorForm?.category || '').trim();
      if (formCat) userCats.add(formCat);
      // Preserve preset order, then append user-cats that aren't
      // already in the preset list.
      const all = [...preset];
      const seen = new Set(preset);
      for (const c of [...userCats].sort((a, b) => a.localeCompare(b))) {
        if (!seen.has(c)) { all.push(c); seen.add(c); }
      }
      return all;
    },

    // === Markdown editor helpers (reusable for cf-groups, profile
    // editor, etc. later). Operate on a target <textarea> element so
    // the same toolbar can drive any markdown input. No external
    // library - basic wrap/prepend selection mutation. ===

    // Wrap the textarea's selected text with leading + trailing
    // strings (e.g. "**" + "**" for bold). When nothing is selected,
    // inserts the wrap markers and places the cursor between them so
    // the user can type immediately.
    mdWrapSelection(textareaSelector, before, after) {
      const ta = typeof textareaSelector === 'string'
        ? document.querySelector(textareaSelector)
        : textareaSelector;
      if (!ta) return;
      const start = ta.selectionStart;
      const end = ta.selectionEnd;
      const sel = ta.value.slice(start, end);
      const wrapped = before + sel + after;
      ta.value = ta.value.slice(0, start) + wrapped + ta.value.slice(end);
      // Dispatch input so x-model picks up the change
      ta.dispatchEvent(new Event('input', { bubbles: true }));
      ta.focus();
      const caret = sel ? start + wrapped.length : start + before.length;
      ta.setSelectionRange(caret, caret);
    },

    // Prepend a string ("- " / "1. ") to each line in the selection.
    // Selection start gets snapped back to the line's first char and
    // selection end gets extended forward to the next newline (or
    // EOF) so the whole last line gets prefixed even when the user
    // selected mid-line. Empty selection prefixes just the line under
    // the caret.
    mdPrependLines(textareaSelector, prefix) {
      const ta = typeof textareaSelector === 'string'
        ? document.querySelector(textareaSelector)
        : textareaSelector;
      if (!ta) return;
      const start = ta.selectionStart;
      const end = ta.selectionEnd;
      const lineStart = ta.value.lastIndexOf('\n', start - 1) + 1;
      const nextNewline = ta.value.indexOf('\n', end);
      const lineEnd = nextNewline === -1 ? ta.value.length : nextNewline;
      const block = ta.value.slice(lineStart, lineEnd);
      const lines = block.split('\n').map(l => prefix + l).join('\n');
      ta.value = ta.value.slice(0, lineStart) + lines + ta.value.slice(lineEnd);
      ta.dispatchEvent(new Event('input', { bubbles: true }));
      ta.focus();
      ta.setSelectionRange(lineStart, lineStart + lines.length);
    },

    // Open the inline link popover anchored to the markdown editor.
    // Captures the current textarea selection so it survives the user
    // focusing into the URL input. Commit / cancel handled by
    // mdLinkConfirm / mdLinkCancel.
    mdInsertLink(textareaSelector) {
      const ta = typeof textareaSelector === 'string'
        ? document.querySelector(textareaSelector)
        : textareaSelector;
      if (!ta) return;
      this.cfMdLinkPopover = {
        open: true,
        target: textareaSelector,
        url: 'https://',
        selStart: ta.selectionStart,
        selEnd: ta.selectionEnd,
      };
      this.$nextTick(() => {
        const input = document.getElementById('cf-md-link-url');
        if (input) { input.focus(); input.select(); }
      });
    },

    mdLinkCancel() {
      this.cfMdLinkPopover = { open: false, target: null, url: '', selStart: 0, selEnd: 0 };
    },

    mdLinkConfirm() {
      const pop = this.cfMdLinkPopover;
      if (!pop || !pop.open) return;
      const url = (pop.url || '').trim();
      if (!url) { this.mdLinkCancel(); return; }
      const ta = typeof pop.target === 'string'
        ? document.querySelector(pop.target)
        : pop.target;
      if (!ta) { this.mdLinkCancel(); return; }
      const sel = ta.value.slice(pop.selStart, pop.selEnd) || 'link';
      const replacement = `[${sel}](${url})`;
      ta.value = ta.value.slice(0, pop.selStart) + replacement + ta.value.slice(pop.selEnd);
      ta.dispatchEvent(new Event('input', { bubbles: true }));
      this.mdLinkCancel();
      ta.focus();
      ta.setSelectionRange(pop.selStart + 1, pop.selStart + 1 + sel.length);
    },

    // Render a minimal markdown subset (bold, italic, code, links,
    // bullet/numbered lists, paragraphs) plus TRaSH's ^^underline^^
    // syntax. Output goes through sanitizeHTML so only safe tags
    // survive (the allow-list in utils/csrf.js already covers
    // a, b, em, i, strong, u, br, p, code, ul, ol, li).
    renderMarkdownPreview(text) {
      if (!text) return '<em style="color:var(--text-muted)">Nothing to preview.</em>';
      let html = String(text);
      // Code spans - do FIRST so the chars inside aren't re-processed
      html = html.replace(/`([^`\n]+)`/g, '<code>$1</code>');
      // TRaSH ^^underline^^
      html = html.replace(/\^\^([^^\n]+?)\^\^/g, '<u>$1</u>');
      // Bold + italic
      html = html.replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>');
      html = html.replace(/\*([^*\n]+)\*/g, '<em>$1</em>');
      // Links [text](url) - URL portion allows ONE level of balanced
      // parens so Wikipedia disambiguators like
      // `(streaming_service)` in
      // `https://en.wikipedia.org/wiki/VRV_(streaming_service)` survive.
      // Optional trailing Jekyll attribute block `{:attr="val"...}` gets
      // consumed and discarded so it doesn't leak as plain text in the
      // CF editor description preview.
      html = html.replace(/\[([^\]]+)\]\((https?:\/\/(?:[^\s()]|\([^()]*\))+)\)(\{:[^}]*\})?/g, '<a href="$2" target="_blank" rel="noopener">$1</a>');
      // Lists - split into blocks, detect "- " or "1. " prefix per line
      const blocks = html.split(/\n\n+/).map(block => {
        const lines = block.split('\n');
        if (lines.every(l => /^\s*-\s+/.test(l))) {
          return '<ul>' + lines.map(l => '<li>' + l.replace(/^\s*-\s+/, '') + '</li>').join('') + '</ul>';
        }
        if (lines.every(l => /^\s*\d+\.\s+/.test(l))) {
          return '<ol>' + lines.map(l => '<li>' + l.replace(/^\s*\d+\.\s+/, '') + '</li>').join('') + '</ol>';
        }
        return '<p>' + lines.join('<br>') + '</p>';
      });
      const out = blocks.join('');
      return sanitizeHTML(out);
    },

    // Build the SAFE HTML payload for the row's hover tooltip.
    // Description goes through sanitizeHTML at construction time so
    // this function returns guaranteed-clean output - callers don't
    // have to remember to sanitize again. TRaSH/JSON link URLs are
    // constructed from helper-emitted strings (no user input), but we
    // still escape via a quick attribute-safe encoder before
    // concatenation to harden against future bugs in those helpers.
    // Switch the Browse list between description-only (with per-row
    // click-to-expand conditions) and always-inline conditions.
    // Closes any open expand panel when leaving description mode so
    // we do not strand state the user can no longer see.
    setCFBrowseViewMode(mode) {
      if (mode !== 'description' && mode !== 'conditions') return;
      this.cfBrowseViewMode = mode;
      if (mode !== 'description') this.cfBrowseExpandedCF = '';
      try { localStorage.setItem('clonarr_cfBrowseViewMode', mode); } catch (_) {}
    },
    // Toggle the per-row conditions panel on the Browse list. Single-
    // open: clicking row B closes row A. Re-clicking the open row
    // collapses it. Empty trash_id = nothing expanded. Only meaningful
    // in description view mode.
    toggleCFBrowseConditions(trashId) {
      if (!trashId) return;
      if (this.cfBrowseViewMode !== 'description') return;
      this.cfBrowseExpandedCF = this.cfBrowseExpandedCF === trashId ? '' : trashId;
    },

    // Inline description HTML for the CF browse row's description cell.
    // Differs from buildCFInfoHTML (tooltip view) by wrapping pieces in
    // dedicated classes so per-row CSS can control sizing - links sit
    // in a small muted footer instead of inheriting body font-size.
    // Falls back to "No description" when both fields are empty so the
    // cell is never blank.
    cfInlineDescriptionHTML(cf, appType, opts = {}) {
      // Description resolve chain - row data first, then raw cfBrowseData
      // (data.cfs / data.customCFs) as fallback. getCFBrowseGroups carries
      // description forward but only for the path it was loaded under;
      // for some load orders the row gets built before descriptions
      // arrive, so fall back to the raw source if row.description is empty.
      let raw = cf?.description || '';
      if (!raw && cf?.trashId) {
        const src = this.cfBrowseData?.[appType];
        if (src) {
          const trashHit = (src.cfs || []).find(c => c.trash_id === cf.trashId);
          if (trashHit?.description) raw = trashHit.description;
          if (!raw) {
            const customHit = (src.customCFs || []).find(c => c.trashId === cf.trashId);
            if (customHit?.description) raw = customHit.description;
          }
        }
      }
      const desc = raw.replace(/\^\^([^^\n]+?)\^\^/g, '<u>$1</u>');
      const safeDesc = sanitizeHTML(desc);
      const esc = (u) => String(u).replace(/[<>"'&]/g, c => ({ '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;', '&':'&amp;' }[c]));
      let html = '';
      if (safeDesc) {
        html += `<span class="cf-desc-text">${safeDesc}</span>`;
      } else {
        html += `<span class="cf-desc-empty">No description</span>`;
      }
      // Meta footer row - TRaSH guide / JSON links plus the rename
      // indicator, all in the same muted line. The rename chip surfaces
      // includeCustomFormatWhenRenaming so the profile editor shows, right
      // where the links live, which CFs append their name to renamed files.
      // It renders for custom CFs too (they can carry the flag). The Custom
      // Formats tab passes opts.hideRename because it already shows this as a
      // badge in the CF name cluster - avoids a duplicate.
      const metaBits = [];
      if (!cf?.isCustom) {
        const guideUrl = this.trashCFGuideUrl ? this.trashCFGuideUrl(cf, appType) : '';
        const jsonUrl = this.trashCFJsonUrl ? this.trashCFJsonUrl(cf, appType) : '';
        if (guideUrl) metaBits.push(`<a href="${esc(guideUrl)}" target="_blank" rel="noopener noreferrer">TRaSH guide</a>`);
        if (jsonUrl) metaBits.push(`<a href="${esc(jsonUrl)}" target="_blank" rel="noopener noreferrer">JSON</a>`);
      } else {
        const tid = cf?.trashId || cf?.id || '';
        if (tid) {
          metaBits.push(`<button type="button" class="cf-desc-json-btn" data-cf-id="${esc(tid)}">JSON</button>`);
        }
      }
      if (!opts.hideRename && cf?.includeInRename) {
        metaBits.push(`<span class="cf-desc-rename" title="Custom Format name is appended to renamed files when this CF matches.">rename</span>`);
      }
      if (metaBits.length) {
        html += `<span class="cf-desc-links">${metaBits.join(' · ')}</span>`;
      }
      return html;
    },

    buildCFInfoHTML(cf, appType) {
      const desc = (cf?.description || '').replace(/\^\^([^^\n]+?)\^\^/g, '<u>$1</u>');
      const safeDesc = sanitizeHTML(desc);
      let html = safeDesc;
      const esc = (u) => String(u).replace(/[<>"'&]/g, c => ({ '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;', '&':'&amp;' }[c]));
      const links = [];
      if (!cf?.isCustom) {
        const guideUrl = this.trashCFGuideUrl ? this.trashCFGuideUrl(cf, appType) : '';
        const jsonUrl = this.trashCFJsonUrl ? this.trashCFJsonUrl(cf, appType) : '';
        if (guideUrl) links.push(`<a href="${esc(guideUrl)}" target="_blank" rel="noopener">TRaSH guide</a>`);
        if (jsonUrl) links.push(`<a href="${esc(jsonUrl)}" target="_blank" rel="noopener">JSON</a>`);
      } else {
        const tid = cf?.trashId || cf?.id || '';
        if (tid) {
          links.push(`<button type="button" class="cf-desc-json-btn" data-cf-id="${esc(tid)}">JSON</button>`);
        }
      }
      if (links.length) html += (safeDesc ? '<br><br>' : '') + links.join(' &nbsp;·&nbsp; ');
      return html;
    },

    // Step 1 of clone: open the name-prompt modal. User chooses the
    // new name + clicks Save (commitCloneCF) or Cancel (cancelCloneCF
    // / outside-click). Nothing is created server-side until commit.
    cloneCFRow(cf, appType) {
      const baseName = (cf?.name || 'Custom Format').replace(/\s+\(Copy(?:\s+\d+)?\)$/, '');
      // Walk existing CFs to avoid proposing a name that already exists.
      // First try "Name (Copy)"; if taken, try "Name (Copy 2)", "(Copy 3)",
      // etc. Without this the user gets a confusing 409 collision error
      // on Save when they're just trying to make a second clone of the
      // same source CF. Must include BOTH the TRaSH catalog (cfs) AND
      // the user's own custom CFs (customCFs) - the previous clone went
      // into customCFs, so checking only cfs misses the collision.
      const data = this.cfBrowseData?.[appType] || {};
      const existingNames = new Set([
        ...(data.cfs || []).map(c => c.name),
        ...(data.customCFs || []).map(c => c.name),
      ]);
      let candidate = `${baseName} (Copy)`;
      let counter = 2;
      while (existingNames.has(candidate)) {
        candidate = `${baseName} (Copy ${counter})`;
        counter++;
      }
      this.cloneModal = {
        open: true,
        sourceCF: cf,
        sourceAppType: appType,
        name: candidate,
        saving: false,
        error: '',
      };
      // Focus the name input after Alpine has rendered the modal.
      this.$nextTick(() => {
        const el = document.getElementById('clone-cf-name-input');
        if (el) { el.focus(); el.select(); }
      });
    },

    cancelCloneCF() {
      this.cloneModal = { open: false, sourceCF: null, sourceAppType: '', name: '', saving: false, error: '' };
    },

    // Open the Add-to-Arr modal for the given CF (either a TRaSH CF or
    // a user custom CF - discriminated by cf.isCustom). Defaults the
    // instance picker to the first instance of the active app type
    // alphabetically; user can switch via the dropdown.
    openAddCFToArr(cf, appType) {
      const insts = (this.instances || []).filter(i => i.type === appType);
      const defaultId = insts.length > 0 ? insts.slice().sort((a, b) => a.name.localeCompare(b.name))[0].id : '';
      this.addCFToArrModal = {
        open: true,
        cfName: cf?.name || '',
        trashId: cf?.isCustom ? '' : (cf?.trashId || ''),
        customCFId: cf?.isCustom ? (cf?.id || '') : '',
        appType,
        instanceId: defaultId,
        target: 'arr',
        ruleId: '',
        score: 0,
        saving: false,
        error: '',
      };
      // Lazy-load cf-sync-rules so the profile-picker filter knows
      // which profiles already manage this CF (excluded from the
      // picker so the user does not accidentally double-add). Cheap
      // no-op if already loaded.
      if (typeof this.loadCFSyncRules === 'function' && !this.cfSyncRulesLoaded?.[appType]) {
        this.loadCFSyncRules(appType);
      }
      // Same for the rule list - modal opens from Browse, which does
      // not necessarily trigger loadAutoSyncRules on its own. Without
      // this the picker would render before autoSyncRules populates
      // and look empty until Alpine reacted to a later refresh.
      if (typeof this.loadAutoSyncRules === 'function' && (!this.autoSyncRules || this.autoSyncRules.length === 0)) {
        this.loadAutoSyncRules();
      }
    },

    cancelAddCFToArr() {
      this.addCFToArrModal = { open: false, cfName: '', trashId: '', customCFId: '', appType: '', instanceId: '', target: 'arr', ruleId: '', score: 0, saving: false, error: '' };
    },

    // Identifier the backend's add-cf endpoint expects: raw TRaSH hash
    // for catalog CFs, "custom:<hex>" for user customs. Mirrors what
    // the rest of the codebase already stores in SelectedCFs.
    addCFModalCFID() {
      const m = this.addCFToArrModal;
      if (!m) return '';
      if (m.trashId) return m.trashId;
      if (m.customCFId) return m.customCFId.startsWith('custom:') ? m.customCFId : 'custom:' + m.customCFId;
      return '';
    },

    // Sync rules for the modal's instance whose profile does NOT yet
    // manage this CF. cfSyncRules data carries per-instance "profiles"
    // entries that list every (ruleId, arr-profile-name) pair already
    // pushing the CF; we filter those out so the user never lands on
    // an already-managed profile (which would 409 on submit).
    addCFAvailableRules() {
      const m = this.addCFToArrModal;
      if (!m || !m.instanceId || !m.appType) return [];
      const cfId = this.addCFModalCFID();
      if (!cfId) return [];
      const rows = this.cfSyncRules?.[m.appType] || [];
      const row = rows.find(r => r.trashId === cfId);
      const managedRuleIds = new Set();
      if (row) {
        const inst = (row.instances || []).find(i => i.id === m.instanceId);
        for (const p of (inst?.profiles || [])) {
          if (p.ruleId) managedRuleIds.add(p.ruleId);
        }
      }
      const out = [];
      for (const rule of (this.autoSyncRules || [])) {
        if (rule.instanceId !== m.instanceId) continue;
        if (rule.orphanedAt) continue;
        if (managedRuleIds.has(rule.id)) continue;
        // Resolve a display name: persisted Arr profile name if we have
        // a sync history entry, else the TRaSH profile name. syncHistory
        // is an object keyed by instanceId, not a flat array.
        const hist = (this.syncHistory?.[rule.instanceId] || []).find(h => h.arrProfileId === rule.arrProfileId);
        const name = hist?.arrProfileName || rule.trashProfileName || `Arr profile #${rule.arrProfileId}`;
        out.push({ id: rule.id, name, arrProfileId: rule.arrProfileId });
      }
      return out.sort((a, b) => a.name.localeCompare(b.name));
    },

    // TRaSH-context default score for this CF + selected rule's
    // profile. Falls back to "default" context, then 0. Used to
    // pre-fill the Score input when the user picks a profile so the
    // common case of "use TRaSH suggested score" is a single click.
    addCFResolveDefaultScore() {
      const m = this.addCFToArrModal;
      if (!m || !m.appType) return 0;
      const cfId = m.trashId; // customs have no trash_scores map
      if (!cfId) return 0;
      const cf = this.cfBrowseData?.[m.appType]?.cfs?.find(c => c.trash_id === cfId);
      const scores = cf?.trash_scores || {};
      if (m.ruleId) {
        const rule = (this.autoSyncRules || []).find(r => r.id === m.ruleId);
        if (rule) {
          // Try resolving via the profile's score-set context.
          const trashId = rule.trashProfileId;
          const ad = this.cfBrowseData?.[m.appType];
          // appData.profiles is not on cfBrowseData; fall through to
          // walking trashProfiles instead.
          const profile = (this.trashProfiles?.[m.appType] || []).find(p => p.trashId === trashId);
          const ctx = profile?.trashScoreSet;
          if (ctx && typeof scores[ctx] === 'number') return scores[ctx];
        }
      }
      if (typeof scores.default === 'number') return scores.default;
      return 0;
    },

    // Called when the user switches target=profile or picks a
    // different rule from the picker so the Score input refreshes to
    // the new context's TRaSH default.
    addCFRefreshDefaultScore() {
      this.addCFToArrModal.score = this.addCFResolveDefaultScore();
    },

    // Instances of the modal's app type, sorted alphabetically for a
    // stable picker. Recomputed on each call rather than cached because
    // the modal is short-lived and the instances list is small.
    addCFToArrInstances() {
      const at = this.addCFToArrModal?.appType;
      if (!at) return [];
      return (this.instances || []).filter(i => i.type === at).sort((a, b) => a.name.localeCompare(b.name));
    },

    // Same list minus instances that already have this CF on Arr (by
    // any means - synced via a rule, pushed via "+", or added outside
    // clonarr). Used by the "Arr instance only" picker so users do
    // not see options that would no-op. cfSyncRules[appType] is the
    // source of truth; if it has not loaded yet the list is the same
    // as addCFToArrInstances (we will not block the user on cold
    // state).
    addCFToArrEligibleInstances() {
      const m = this.addCFToArrModal;
      if (!m || !m.appType) return [];
      const all = this.addCFToArrInstances();
      const cfId = this.addCFModalCFID();
      if (!cfId) return all;
      const rows = this.cfSyncRules?.[m.appType] || [];
      const row = rows.find(r => r.trashId === cfId);
      if (!row) return all;
      const presentOn = new Set();
      for (const inst of (row.instances || [])) {
        if (inst?.id) presentOn.add(inst.id);
      }
      return all.filter(i => !presentOn.has(i.id));
    },

    // Whether the modal's currently-selected instance already has the
    // CF on Arr (via any path - sync rule, "+" Add, or external).
    // Drives the "already there" info card in Arr-only target mode so
    // users do not re-click Add and get a silent no-op. Cold state
    // (cfSyncRules not yet loaded) reports false to avoid blocking
    // the user before the data arrives.
    addCFAlreadyOnSelectedInstance() {
      const m = this.addCFToArrModal;
      if (!m || !m.appType || !m.instanceId) return false;
      const cfId = this.addCFModalCFID();
      if (!cfId) return false;
      const rows = this.cfSyncRules?.[m.appType] || [];
      const row = rows.find(r => r.trashId === cfId);
      if (!row) return false;
      return (row.instances || []).some(i => i?.id === m.instanceId);
    },

    // "Add to Radarr (main)" / "Add to Sonarr 4K" - surfaces the
    // selected instance name in the primary button so the user can see
    // where the CF is going without scanning the dropdown.
    addCFToArrButtonLabel() {
      const m = this.addCFToArrModal;
      if (!m || !m.instanceId) return 'Add';
      const inst = (this.instances || []).find(i => i.id === m.instanceId);
      if (!inst) return 'Add';
      return 'Add to ' + inst.name;
    },

    async commitAddCFToArr() {
      const m = this.addCFToArrModal;
      if (!m || !m.instanceId || m.saving) return;
      this.addCFToArrModal.saving = true;
      this.addCFToArrModal.error = '';
      try {
        const body = m.trashId ? { trashIds: [m.trashId] } : { customCFIds: [m.customCFId] };
        const r = await fetch(`/api/instances/${m.instanceId}/cfs/add`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body),
        });
        if (!r.ok) {
          let msg = 'Failed to add CF to Arr';
          try { const j = await r.json(); if (j?.error) msg = j.error; } catch (_) {}
          this.addCFToArrModal.error = msg;
          this.addCFToArrModal.saving = false;
          return;
        }
        const result = await r.json();
        const instName = ((this.instances || []).find(i => i.id === m.instanceId)?.name) || 'Arr';
        if (result.added?.length > 0) {
          this.showToast(`Added "${m.cfName}" to ${instName}.`, 'success', 4000);
        } else if (result.skipped?.length > 0) {
          this.showToast(`"${m.cfName}" already exists on ${instName} - skipped.`, 'info', 4000);
        } else if (result.failed?.length > 0) {
          this.addCFToArrModal.error = result.failed[0].error || 'Add failed';
          this.addCFToArrModal.saving = false;
          return;
        }
        this.cancelAddCFToArr();
      } catch (e) {
        this.addCFToArrModal.error = e?.message || 'Network error';
        this.addCFToArrModal.saving = false;
      }
    },

    // Append the CF to a sync rule's SelectedCFs + ScoreOverrides.
    // applyAndSync=true also kicks off a sync against the rule's Arr
    // profile right after the rule mutation lands (same trigger the
    // editor's Apply & Sync uses), so the user sees the CF on Arr in
    // one click. Otherwise the rule stays "unsynced" until the next
    // scheduled sync / manual Update.
    async commitAddCFToProfile(applyAndSync) {
      const m = this.addCFToArrModal;
      if (!m || m.saving) return;
      if (!m.ruleId) {
        this.addCFToArrModal.error = 'Pick a profile first.';
        return;
      }
      const cfId = this.addCFModalCFID();
      if (!cfId) {
        this.addCFToArrModal.error = 'CF id missing.';
        return;
      }
      const rule = (this.autoSyncRules || []).find(r => r.id === m.ruleId);
      if (!rule) {
        this.addCFToArrModal.error = 'Rule no longer exists.';
        return;
      }
      this.addCFToArrModal.saving = true;
      this.addCFToArrModal.error = '';
      try {
        const r = await fetch(`/api/auto-sync/rules/${m.ruleId}/add-cf`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ cfId, score: Number(m.score) || 0 }),
        });
        if (!r.ok) {
          let msg = 'Failed to add CF to profile';
          try { const j = await r.json(); if (j?.error) msg = j.error; } catch (_) {}
          this.addCFToArrModal.error = msg;
          this.addCFToArrModal.saving = false;
          return;
        }
        await r.json();
        // Re-fetch rules so subsequent opens of this modal filter the
        // picker correctly. Cheap enough to do unconditionally.
        if (typeof this.loadAutoSyncRules === 'function') {
          await this.loadAutoSyncRules();
        }
        const instName = ((this.instances || []).find(i => i.id === m.instanceId)?.name) || 'Arr';
        // Resolve the Arr-side profile name the same way ruleToHistoryShape
        // does (profiles.js:2626) so toasts read identically across flows.
        // Falls back to the picker label, then "Arr profile #N".
        const arrName = (typeof this.resolveArrProfileName === 'function')
          ? this.resolveArrProfileName(rule.instanceId, rule.arrProfileId)
          : null;
        const pickerLabel = this.addCFAvailableRules().find(r => r.id === m.ruleId)?.name;
        const profileName = arrName
          ? `${arrName} (#${rule.arrProfileId})`
          : (pickerLabel || `Arr profile #${rule.arrProfileId}`);
        if (applyAndSync) {
          // Reuse the same dry-run-then-apply path the editor uses by
          // delegating to quickSync, which knows how to build the body
          // from a rule + arrProfileId pair. quickSync re-resolves
          // selectedCFs / scoreOverrides / etc. from the live rule, so
          // sh only needs the identification fields + the display
          // strings quickSync uses in toasts.
          if (typeof this.quickSync === 'function') {
            const sh = {
              arrProfileId: rule.arrProfileId,
              arrProfileName: arrName || null,
              profileName,
              profileTrashId: rule.trashProfileId || '',
              importedProfileId: rule.importedProfileId || '',
              selectedCFs: (rule.selectedCFs || []).reduce((acc, t) => (acc[t] = true, acc), {}),
              excludedCFs: (rule.excludedCFs || []).slice(),
              keepArrCFIDs: rule.keepArrCFIDs || null,
              scoreOverrides: rule.scoreOverrides || null,
              qualityOverrides: rule.qualityOverrides || null,
              qualityStructure: rule.qualityStructure || null,
              overrides: rule.overrides || null,
              behavior: rule.behavior || null,
            };
            const inst = (this.instances || []).find(i => i.id === m.instanceId);
            if (inst) await this.quickSync(inst, sh);
          }
          this.showToast(`Added "${m.cfName}" to ${profileName} and synced to ${instName}.`, 'success', 4500);
        } else {
          this.showToast(`Added "${m.cfName}" to ${profileName}. Will sync on next Update / Auto-sync.`, 'success', 4500);
        }
        if (typeof this.loadCFSyncRules === 'function') {
          this.loadCFSyncRules(m.appType);
        }
        this.cancelAddCFToArr();
      } catch (e) {
        this.addCFToArrModal.error = e?.message || 'Network error';
        this.addCFToArrModal.saving = false;
      }
    },

    // Edit-mode alternative to commitCloneCF: instead of POSTing the
    // new CF straight away, this opens the CF editor pre-filled with
    // the source CF's specs / scores / flags + the auto-suffixed name
    // from the modal. The user reviews/edits in the full editor and
    // saves from there (saveCFEditor handles the POST). Useful when
    // the user wants to tweak conditions before committing the clone.
    async openCloneInEditor() {
      const m = this.cloneModal;
      if (!m || !m.sourceCF) return;
      const newName = (m.name || '').trim();
      if (!newName) {
        this.cloneModal.error = 'Name is required';
        return;
      }
      const cf = m.sourceCF;
      const appType = m.sourceAppType;

      // Resolve source data the same way commitCloneCF does so the
      // editor sees identical specs / flags / scores whether the
      // source is a custom CF or a TRaSH catalog entry.
      let rawSpecs = cf?.specifications || [];
      let trashSourceCF = null;
      if (cf?.trashId && !cf?.isCustom) {
        const cfs = this.cfBrowseData?.[appType]?.cfs || [];
        trashSourceCF = cfs.find(c => c.trash_id === cf.trashId) || null;
        if (!rawSpecs.length) rawSpecs = trashSourceCF?.specifications || [];
      }

      const includeInRename = !!(cf?.rawCustom?.includeInRename
        ?? trashSourceCF?.includeCustomFormatWhenRenaming
        ?? cf?.includeCustomFormatWhenRenaming);

      // trash_scores lives at different keys depending on source:
      // TRaSH catalog uses snake_case (trash_scores), custom CFs use
      // camelCase (trashScores). Pick whichever is non-empty.
      const scoresMap = cf?.rawCustom?.trashScores
        || trashSourceCF?.trash_scores
        || cf?.trash_scores
        || {};

      const description = cf?.rawCustom?.description
        || trashSourceCF?.description
        || cf?.description
        || '';

      // Close the modal first so the editor takes focus cleanly.
      this.cancelCloneCF();

      // Boot the editor in CREATE mode (so Save creates a new CF
      // rather than overwriting the source), then overwrite the
      // empty form openCFEditor seeded with the pre-filled clone
      // data. arrSpecToEditorSpec normalises TRaSH-shape fields into
      // the Arr-shape array the editor expects.
      await this.openCFEditor('create', appType);
      this.cfEditorForm = {
        id: '',
        name: newName,
        appType,
        category: 'Custom',
        newCategory: '',
        includeInRename,
        specifications: (rawSpecs || []).map(s => this.arrSpecToEditorSpec(s)),
        trashId: '',
        trashScores: Object.entries(scoresMap).map(([k, v]) => ({
          _key: ++this.cfEditorScoreCounter, context: k, score: v,
        })),
        description,
      };
    },

    // Step 2 of clone: actually POST the new CF using the user's
    // chosen name. Endpoint + body shape mirror the create flow in
    // saveCFEditor (POST /api/custom-cfs with { cfs: [payload] }).
    // Specs need their `fields` normalised: the TRaSH catalog API
    // returns `fields: { value: ... }` (TRaSH JSON shape) while the
    // create handler expects `fields: [{ name, value }]` (Arr shape).
    async commitCloneCF() {
      const m = this.cloneModal;
      if (!m || !m.sourceCF) return;
      const newName = (m.name || '').trim();
      if (!newName) {
        this.cloneModal.error = 'Name is required';
        return;
      }
      this.cloneModal.saving = true;
      this.cloneModal.error = '';
      const cf = m.sourceCF;
      const appType = m.sourceAppType;

      // Source specs: prefer cf.specifications (rich row), fall back
      // to looking the CF up in the catalog by trash_id. Hold the
      // looked-up record too so includeInRename can fall back when the
      // source row is a TRaSH catalog entry (no .rawCustom).
      let rawSpecs = cf?.specifications || [];
      let trashSourceCF = null;
      if (cf?.trashId && !cf?.isCustom) {
        const cfs = this.cfBrowseData?.[appType]?.cfs || [];
        trashSourceCF = cfs.find(c => c.trash_id === cf.trashId) || null;
        if (!rawSpecs.length) rawSpecs = trashSourceCF?.specifications || [];
      }

      const specifications = rawSpecs.map(s => {
        let fields = s.fields;
        if (typeof fields === 'string') {
          try { fields = JSON.parse(fields); } catch (_) { fields = []; }
        }
        // TRaSH catalog shape `{ value: X }` → Arr shape `[{name:"value", value:X}]`
        if (fields && !Array.isArray(fields) && typeof fields === 'object') {
          fields = Object.entries(fields).map(([name, value]) => ({ name, value }));
        }
        if (!Array.isArray(fields)) fields = [];
        return {
          name: s.name || '',
          implementation: s.implementation,
          negate: !!s.negate,
          required: !!s.required,
          fields,
        };
      });

      // includeInRename: custom CFs have it on rawCustom; TRaSH catalog
      // entries expose it as includeCustomFormatWhenRenaming. Honour
      // whichever shape the source provides so a clone of a TRaSH CF
      // that marks "include in rename" carries the flag forward.
      const includeInRename = !!(cf?.rawCustom?.includeInRename
        ?? trashSourceCF?.includeCustomFormatWhenRenaming
        ?? cf?.includeCustomFormatWhenRenaming);
      const payload = {
        name: newName,
        appType,
        category: 'Custom',
        includeInRename,
        specifications,
        trashId: '',
      };

      try {
        const res = await fetch('/api/custom-cfs', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ cfs: [payload] }),
        });
        if (!res.ok) {
          let errMsg = `Clone failed (HTTP ${res.status})`;
          try { const err = await res.json(); errMsg = err.error || errMsg; } catch (_) {}
          this.cloneModal.error = errMsg;
          this.cloneModal.saving = false;
          return;
        }
        this.showToast ? this.showToast(`Cloned as "${newName}"`, 'success', 4000) : null;
        await this.loadCFBrowse(appType);
        this.cancelCloneCF();
      } catch (e) {
        this.cloneModal.error = 'Clone failed: ' + (e?.message || e);
        this.cloneModal.saving = false;
      }
    },

    // --- CF Editor (Create/Edit) ---

    // True when the name typed in the CF Editor is byte-exact match
    // against a TRaSH-published CF for the same app. Drives the small
    // "guide" badge next to the Name field. Save is NEVER blocked -
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
      this.cfEditorSpecCounter = 0;
      this.cfEditorScoreCounter = 0;
      this.cfEditorActiveTab = 'general';
      this.cfEditorDescriptionPreview = false;
      this.cfMdLinkPopover = { open: false, target: null, url: '', selStart: 0, selEnd: 0 };
      this._cfEditorBaseline = null;

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
          this.showToast('Custom CF not found - it may have been deleted', 'error', 8000);
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
          trashScores: Object.entries(full.trashScores || {}).map(([k,v]) => ({_key: ++this.cfEditorScoreCounter, context:k, score:v})),
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
      // Capture a baseline snapshot of the editable surface so
      // cfEditorIsDirty() can detect when the user has changed
      // anything. Used by closeCFEditor / ESC to decide whether to
      // prompt before discarding work. Re-captured after every
      // successful save so post-save "is dirty?" returns false.
      this._cfEditorCaptureBaseline();
    },

    // Take a JSON snapshot of every editable field. Used as the
    // dirty-tracking baseline. Includes specifications (with field
    // values) + trashScores + name/category/etc.
    _cfEditorCaptureBaseline() {
      try {
        this._cfEditorBaseline = JSON.stringify(this._cfEditorSnapshot());
      } catch (_) {
        this._cfEditorBaseline = null;
      }
    },

    _cfEditorSnapshot() {
      const f = this.cfEditorForm || {};
      return {
        name: f.name || '',
        category: f.category || '',
        newCategory: f.newCategory || '',
        includeInRename: !!f.includeInRename,
        description: f.description || '',
        trashId: f.trashId || '',
        // Specs: strip the internal `_key` field (volatile, not user data)
        specs: (f.specifications || []).map(s => ({
          name: s.name || '',
          implementation: s.implementation || '',
          negate: !!s.negate,
          required: !!s.required,
          fields: (s.fields || []).map(fd => ({ name: fd.name, value: fd.value })),
        })),
        scores: (f.trashScores || []).map(t => ({ context: t.context, score: t.score })),
      };
    },

    // True when the editor's form differs from its baseline. Returns
    // false when no baseline was ever captured (defensive - shouldn't
    // block close in degraded mode).
    cfEditorIsDirty() {
      if (!this._cfEditorBaseline) return false;
      try {
        return JSON.stringify(this._cfEditorSnapshot()) !== this._cfEditorBaseline;
      } catch (_) {
        return false;
      }
    },

    // Cancel path - prompts before closing when the editor has
    // unsaved changes. Used by the Cancel button + ESC key.
    closeCFEditor() {
      if (!this.cfEditorIsDirty()) {
        this.showCFEditor = false;
        this._cfEditorBaseline = null;
        return;
      }
      const name = (this.cfEditorForm?.name || '').trim() || 'this custom format';
      this.confirmModal = {
        show: true,
        title: 'Discard unsaved changes?',
        message: `You have unsaved edits to "${name}". Close the editor and discard them?\n\nThe saved copy on disk (if any) is unaffected.`,
        confirmLabel: 'Discard changes',
        onConfirm: () => {
          this.showCFEditor = false;
          this._cfEditorBaseline = null;
        },
        onCancel: () => {},
      };
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
        // No schema match - fallback to guessing
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
      if (this.cfEditorSchema[appType] && this.cfEditorSchema[appType].length > 0) {
        this.cfEditorSchemaError = '';
        return;
      }

      this.cfEditorSchemaLoading = true;
      this.cfEditorSchemaError = '';
      const label = appType.charAt(0).toUpperCase() + appType.slice(1);
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
          if (parsed.length === 0) {
            this.cfEditorSchemaError = `${label} returned an empty schema. Make sure ${label} is fully started and try again.`;
          } else {
            this.cfEditorSchema = { ...this.cfEditorSchema, [appType]: parsed };
          }
        } else if (res.status === 404) {
          this.cfEditorSchemaError = `No ${label} instance configured. Add one in Settings before creating custom formats.`;
        } else {
          let detail = '';
          try { detail = (await res.json())?.error || ''; } catch(_) {}
          this.cfEditorSchemaError = `Could not reach ${label} (HTTP ${res.status}). ${detail || `Check that ${label} is running and the URL + API key are correct.`}`;
        }
      } catch (e) {
        console.error('Failed to load CF schema:', e);
        this.cfEditorSchemaError = `Could not reach ${label}. ${e.message || 'Network error.'} Make sure ${label} is running.`;
      } finally {
        this.cfEditorSchemaLoading = false;
      }
    },

    // Retry schema fetch from the error banner inside the editor.
    async retryCFEditorSchema() {
      const appType = this.cfEditorForm.appType;
      if (!appType) return;
      // Drop any partial cache so loadCFEditorSchema actually re-fetches
      if (this.cfEditorSchema[appType] && this.cfEditorSchema[appType].length === 0) {
        const { [appType]: _, ...rest } = this.cfEditorSchema;
        this.cfEditorSchema = rest;
      }
      await this.loadCFEditorSchema();
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
      // but pb.cutoff stays empty - export produces `cutoff: ""`. Dispatch
      // a change event so the binding runs. Safe from looping: x-effect's
      // next pass sees pb.cutoff == targetValue and skips the dispatch.
      if (targetValue !== selectedValue) {
        el.dispatchEvent(new Event('change', { bubbles: true }));
      }
    },

    populateCutoffSelect(el, qualityStructure, profile, selectedValue, qualityOverrides) {
      // Two sources depending on mode:
      // 1) STRUCTURE-DRIVEN: qualityStructure has entries - user has grouped or
      //    reordered via Edit Groups. Use allowed flag on each item.
      // 2) LEGACY FLAT-TOGGLE: qualityStructure is empty; user toggles write to
      //    qualityOverrides map keyed by name. Here we MUST apply the overrides
      //    on top of profile.items - otherwise a just-toggled-on resolution
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
        options.push({ value: trashDefault, name: trashDefault + (trashValid ? ' (TRaSH default)' : ' (TRaSH default - not in structure)'), disabled: !trashValid });
      }
      // All allowed items except TRaSH default (avoid duplicate)
      for (const item of items) {
        if (item.name !== trashDefault) options.push({ value: item.name, name: item.name });
      }
      // If the saved cutoff override points to a quality not in the
      // TRaSH-spec items list (user added it via override, imported
      // from Arr, etc.) inject it as a plain option so the dropdown
      // displays it. Without this, el.value never matches any <option>
      // and the browser silently falls back to the first option (TRaSH
      // default), making it look like the override was lost - even
      // though pdOverrides.cutoffQuality still holds the right value
      // and Save & Sync will persist it correctly. No suffix label -
      // it's a legitimate user override, treat it like any other.
      if (selectedValue && selectedValue !== '__skip__' && !options.some(o => o.value === selectedValue)) {
        options.push({ value: selectedValue, name: selectedValue });
      }
      // Skip option
      options.push({ value: '__skip__', name: '- Don\'t sync cutoff -' });
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
      // Prepend, not append: on a CF with many conditions, adding at the bottom
      // forces a scroll to the end every time. New condition goes to the top so
      // it is immediately visible and editable.
      this.cfEditorForm.specifications.unshift({
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
      //      restores the snapshot - covers "I clicked the wrong type,
      //      went elsewhere, came back".
      //   2. Same-named compatible carry: when the new implementation has
      //      a field with the same name + type as the old one and the
      //      history doesn't have a snapshot for it, copy the current
      //      value forward. Covers "two regex-style specs sharing a 'value'
      //      textbox" (ReleaseTitle ↔ ReleaseGroup).
      //
      // The snapshot is taken from the PREVIOUSLY active implementation,
      // which we track via spec._lastImpl. spec._fieldHistory persists for
      // the editor's lifetime - populated either here or by openCFEditor's
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
        // Tier 1: prior visit to this implementation - restore exactly.
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
          // and retry - the trailing reset below is unreachable after
          // this `return`, so reset locally.
          this.cfEditorSaving = false;
          return;
        }

        this.cfEditorResult = { error: false, message: this.cfEditorMode === 'edit' ? 'Updated successfully' : 'Created successfully' };
        // Re-capture baseline so the unsaved-changes guard doesn't
        // fire on the post-save close. closeCFEditor is invoked
        // implicitly via the setTimeout below.
        this._cfEditorCaptureBaseline();
        // Refresh CF browse data
        this.loadCFBrowse(f.appType);
        // Close after brief delay to show success (keep saving state active)
        setTimeout(() => { this.showCFEditor = false; this._cfEditorBaseline = null; this.cfEditorSaving = false; }, 800);
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

      if (f.description) {
        trashJSON.description = f.description;
      }

      this.cfExportTitle = f.name ? `${f.name} — TRaSH JSON` : 'Export TRaSH JSON';
      this.cfExportContent = JSON.stringify(trashJSON, null, 2);
      this.cfExportCopied = false;
    },

    async viewCustomCFJSON(trashId) {
      if (!trashId) return;
      const appType = this.activeAppType || 'radarr';
      let customCF = (this.cfBrowseData?.[appType]?.customCFs || []).find(c => (c.id === trashId || c.trashId === trashId || c.trash_id === trashId));
      if (!customCF) {
        for (const cf of (this.extraCFAllCFs || [])) {
          if ((cf.trashId === trashId || cf.id === trashId) && cf.specifications) {
            customCF = cf;
            break;
          }
        }
      }
      if (!customCF) {
        try {
          const res = await fetch(`/api/custom-cfs?appType=${appType}`);
          if (res.ok) {
            const list = await res.json();
            this.cfBrowseData = {
              ...this.cfBrowseData,
              [appType]: { ...(this.cfBrowseData?.[appType] || {}), customCFs: list }
            };
            customCF = list.find(c => c.id === trashId || c.trashId === trashId);
          }
        } catch (e) {
          console.error('Failed to load custom CF JSON:', e);
        }
      }
      if (!customCF) return;

      const specs = (customCF.specifications || []).map(s => {
        let fields = {};
        if (Array.isArray(s.fields)) {
          fields = Object.fromEntries(s.fields.map(fld => [fld.name, fld.value]));
        } else if (s.fields && typeof s.fields === 'object') {
          fields = s.fields;
        }
        return {
          name: s.name || '',
          implementation: s.implementation || '',
          negate: !!s.negate,
          required: !!s.required,
          fields: fields,
        };
      });

      const trashScores = customCF.trashScores || (customCF.score !== undefined ? { default: customCF.score } : {});

      const trashJSON = {
        trash_id: customCF.trashId || customCF.id || '',
        trash_scores: trashScores,
        name: customCF.name || '',
        includeCustomFormatWhenRenaming: !!customCF.includeInRename,
        specifications: specs,
      };

      if (customCF.description) {
        trashJSON.description = customCF.description;
      }

      this.cfExportTitle = customCF.name ? `${customCF.name} — Custom Format JSON` : 'Custom Format JSON';
      this.cfExportContent = JSON.stringify(trashJSON, null, 2);
      this.cfExportCopied = false;
    },

    // --- Import Custom CFs ---

    // Detect known cross-Arr CF spec incompatibilities. Returns an array of
    // issue objects for display. Only flags objectively-wrong cases or known
    // canonical-name mismatches - never custom-named CFs (we can't know
    // intent there). Empty result = import looks clean for target.
    _detectCrossArrImportIssues(cfs, targetApp) {
      // Spec implementations that exist in only one Arr - the other will
      // reject them at sync. Verified against TRaSH guide CF coverage.
      const ARR_ONLY_SPECS = {
        radarr: ['ReleaseTypeSpecification'],          // Sonarr-only (Single/Multi-episode/Season pack)
        sonarr: ['QualityModifierSpecification'],      // Radarr-only (Remux modifier)
      };
      // Source enum per Arr. Values verified against the canonical enum
      // definitions in each project (Sonarr: QualitySource.cs, Radarr:
      // QualitySource.cs as of develop). Note: Arr serializes only the
      // integer - the name is purely a label, so different naming
      // conventions for the same value are equally valid. Primary-name
      // (index 0) is used in warning messages.
      //
      // Sonarr enum:  Unknown=0, Television=1, TelevisionRaw=2, Web=3,
      //               WebRip=4, DVD=5, Bluray=6, BlurayRaw=7
      // Radarr enum:  UNKNOWN=0, CAM=1, TELESYNC=2, TELECINE=3, WORKPRINT=4,
      //               DVD=5, TV=6, WEBDL=7, WEBRIP=8, BLURAY=9
      //
      // Aliases (after normalize() = lowercase + strip non-alphanumeric):
      //   "Web"/"WEBDL"/"WEB-DL" all → 'web' or 'webdl' (both accepted on
      //   the value that means web-download in each app).
      //   "WebRip"/"WEB-Rip"/"WEBRIP" all → 'webrip'.
      //   "TV"/"Television" interchangeable inside each app where both fit.
      const SOURCE_VALUE_NAMES = {
        radarr: {
          0: ['unknown'],
          1: ['cam'],
          2: ['telesync', 'ts'],
          3: ['telecine', 'tc'],
          4: ['workprint'],
          5: ['dvd'],
          6: ['tv', 'television'],
          7: ['webdl', 'web', 'webrelease'],
          8: ['webrip'],
          9: ['bluray'],
        },
        sonarr: {
          0: ['unknown'],
          1: ['television', 'tv'],
          2: ['televisionraw', 'tvraw'],
          3: ['web', 'webdl', 'webrelease'],
          4: ['webrip'],
          5: ['dvd'],
          6: ['bluray'],
          7: ['blurayraw'],
        },
      };
      // Known canonical Source names - only flag mismatch when spec.name
      // normalizes to one of these (TRaSH uses these). Unknown names =
      // user intent unclear, skip the check entirely.
      const KNOWN_SOURCE = new Set(['webdl','web','webrelease','webrip',
                                    'bluray','blurayraw','remux','blurayremux',
                                    'dvd','television','tv','tvraw','televisionraw',
                                    'cam','telesync','ts','telecine','tc',
                                    'workprint','unknown']);
      // IndexerFlag - TRaSH only uses FreeLeech (1, same in both) and
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
              message: `${impl} doesn't exist in ${targetApp} - will be rejected at sync`
            });
            continue;
          }

          const value = spec.fields?.value;
          if (value === undefined || value === null) continue;

          // Check 2: SourceSpecification - value out of range OR canonical-name mismatch
          if (impl === 'SourceSpecification') {
            const validNames = SOURCE_VALUE_NAMES[targetApp]?.[value];
            if (!validNames) {
              issues.push({
                severity: 'error',
                cf: cf.name, spec: spec.name || '(unnamed)',
                message: `SourceSpecification value=${value} is out of range for ${targetApp}`
              });
            } else {
              const specNorm = normalize(spec.name);
              // Skip when the spec name isn't a known source label (user
              // named it something arbitrary - intent unclear).
              if (KNOWN_SOURCE.has(specNorm) && !validNames.includes(specNorm)) {
                // Name doesn't fit the value in target app - try to find
                // the value where the spec name IS valid, so we can suggest
                // it. This is the "you meant value=X" hint that catches
                // the cross-app silent-mismatch case (e.g. Radarr WEBDL=7
                // imported to Sonarr where 7=BlurayRaw - suggest 3).
                let suggestedValue = null;
                for (const [v, names] of Object.entries(SOURCE_VALUE_NAMES[targetApp] || {})) {
                  if (names.includes(specNorm)) {
                    suggestedValue = parseInt(v, 10);
                    break;
                  }
                }
                const targetPrimary = validNames[0];
                let msg = `Spec named "${spec.name}" with value=${value}, but in ${targetApp} value=${value} means "${targetPrimary}".`;
                if (suggestedValue !== null) {
                  const suggestedPrimary = SOURCE_VALUE_NAMES[targetApp][suggestedValue][0];
                  msg += ` Did you mean value=${suggestedValue} (${suggestedPrimary})?`;
                } else {
                  msg += ` "${spec.name}" has no equivalent in ${targetApp}'s SourceSpecification.`;
                }
                issues.push({
                  severity: 'warning',
                  cf: cf.name, spec: spec.name || '(unnamed)',
                  message: msg,
                });
              }
            }
          }

          // Check 3: IndexerFlagSpecification - Internal flag value mismatch
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
      this.importCFFilter = '';
      this.importCFHideGuide = false;
      this.importCFHideExisting = true;
      this.showImportCFModal = true;
    },

    // Filtered view of importCFList - applies the modal's three filters
    // (free-text name match, hide-guide, hide-already-imported) in one
    // pass. Helpers consume this so the visible count + Select All only
    // act on what the user can actually see.
    importCFListFiltered() {
      const q = (this.importCFFilter || '').trim().toLowerCase();
      return (this.importCFList || []).filter(cf => {
        if (this.importCFHideGuide && cf.trashMatch) return false;
        if (this.importCFHideExisting && cf.exists) return false;
        if (q && !(cf.name || '').toLowerCase().includes(q)) return false;
        return true;
      });
    },

    // Mass-select every importable (non-guide, non-existing) row that's
    // currently visible after filters. Backs the "Select non-guide"
    // shortcut so users can skip the chore of clicking each row.
    importCFSelectNonGuide() {
      for (const cf of this.importCFListFiltered()) {
        if (!cf.exists && !cf.trashMatch) cf.selected = true;
      }
      this.importCFList = [...this.importCFList];
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

        // Don't filter TRaSH-name matches out - the user owns their
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
          // Only same-name-as-existing-custom collisions are skipped -
          // TRaSH-name matches are allowed through (user owns naming).
          const customSkipped = (result.skippedCollisions || []).length;
          const suffix = customSkipped > 0
            ? ` (${customSkipped} skipped - same name as existing custom CF)`
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

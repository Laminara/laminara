use std::collections::{BTreeMap, HashSet};

use serde::{Deserialize, Serialize};

use crate::proto::core::v1::{FeatureGroup, FeatureModel, SelectionType};

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct FeatureSelection {
    #[serde(default)]
    pub selected: BTreeMap<String, Vec<String>>,
}

pub fn optional_paths(model: &Option<FeatureModel>) -> HashSet<String> {
    let mut set = HashSet::new();
    if let Some(model) = model {
        collect_optional(&model.groups, &mut set);
    }
    set
}

fn collect_optional(groups: &[FeatureGroup], set: &mut HashSet<String>) {
    for group in groups {
        for option in &group.options {
            for file in &option.files {
                set.insert(file.clone());
            }
            collect_optional(&option.groups, set);
        }
    }
}

struct ActiveOption {
    order: usize,
    files: Vec<String>,
    requires: Vec<String>,
    incompatible_with: Vec<String>,
    jvm_args: Vec<String>,
    game_args: Vec<String>,
    classpath: Vec<String>,
}

#[derive(Debug, Default)]
pub struct LaunchExtras {
    pub jvm_args: Vec<String>,
    pub game_args: Vec<String>,
    pub classpath: Vec<String>,
}

fn active_options(
    model: &Option<FeatureModel>,
    selection: &FeatureSelection,
) -> BTreeMap<String, ActiveOption> {
    let mut active: BTreeMap<String, ActiveOption> = BTreeMap::new();
    let mut order = 0usize;
    if let Some(model) = model {
        for group in &model.groups {
            walk(group, group.id.clone(), selection, &mut active, &mut order);
        }
    }
    enforce_constraints(&mut active);
    active
}

pub fn resolve_active(
    model: &Option<FeatureModel>,
    selection: &FeatureSelection,
) -> (HashSet<String>, HashSet<String>) {
    let active = active_options(model, selection);

    let addrs: HashSet<String> = active.keys().cloned().collect();
    let files = active
        .values()
        .flat_map(|option| option.files.iter().cloned())
        .collect();
    (addrs, files)
}

pub fn resolve_extras(model: &Option<FeatureModel>, selection: &FeatureSelection) -> LaunchExtras {
    let active = active_options(model, selection);

    let mut ordered: Vec<&ActiveOption> = active.values().collect();
    ordered.sort_by_key(|option| option.order);

    let mut extras = LaunchExtras::default();
    for option in ordered {
        extras.jvm_args.extend(option.jvm_args.iter().cloned());
        extras.game_args.extend(option.game_args.iter().cloned());
        for entry in &option.classpath {
            match crate::sync::validate_manifest_path(entry) {
                Ok(()) => extras.classpath.push(entry.clone()),
                Err(error) => tracing::warn!("classpath entry dropped: {error}"),
            }
        }
    }
    extras
}

fn walk(
    group: &FeatureGroup,
    group_addr: String,
    selection: &FeatureSelection,
    active: &mut BTreeMap<String, ActiveOption>,
    order: &mut usize,
) {
    let chosen = effective(group, &group_addr, selection);
    for option in &group.options {
        if chosen.iter().any(|id| id == &option.id) {
            let option_addr = format!("{group_addr}#{}", option.id);
            let meta = option.meta.as_ref();
            active.insert(
                option_addr.clone(),
                ActiveOption {
                    order: *order,
                    files: option.files.clone(),
                    requires: meta.map(|m| m.requires.clone()).unwrap_or_default(),
                    incompatible_with: meta
                        .map(|m| m.incompatible_with.clone())
                        .unwrap_or_default(),
                    jvm_args: option.jvm_args.clone(),
                    game_args: option.game_args.clone(),
                    classpath: option.classpath.clone(),
                },
            );
            *order += 1;
            for sub in &option.groups {
                walk(
                    sub,
                    format!("{option_addr}/{}", sub.id),
                    selection,
                    active,
                    order,
                );
            }
        }
    }
}

fn enforce_constraints(active: &mut BTreeMap<String, ActiveOption>) {
    let limit = active.len() + 1;
    for _ in 0..limit {
        let Some(victim) = first_violation(active) else {
            return;
        };
        drop_with_descendants(active, &victim);
    }
}

fn first_violation(active: &BTreeMap<String, ActiveOption>) -> Option<String> {
    let mut ordered: Vec<(&String, &ActiveOption)> = active.iter().collect();
    ordered.sort_by_key(|(_, option)| option.order);

    for (addr, option) in &ordered {
        if option
            .requires
            .iter()
            .any(|needed| !active.contains_key(needed))
        {
            return Some((*addr).clone());
        }
    }
    for (index, (addr, option)) in ordered.iter().enumerate() {
        for other in ordered.iter().take(index) {
            let conflicts = option.incompatible_with.iter().any(|a| a == other.0)
                || other.1.incompatible_with.iter().any(|a| &a == addr);
            if conflicts {
                return Some((*addr).clone());
            }
        }
    }
    None
}

fn drop_with_descendants(active: &mut BTreeMap<String, ActiveOption>, addr: &str) {
    let prefix = format!("{addr}/");
    active.retain(|key, _| key != addr && !key.starts_with(&prefix));
}

fn effective(group: &FeatureGroup, addr: &str, selection: &FeatureSelection) -> Vec<String> {
    match selection.selected.get(addr) {
        Some(saved) => {
            let mut ids: Vec<String> = group
                .options
                .iter()
                .map(|option| option.id.clone())
                .filter(|id| saved.contains(id))
                .collect();
            if group.selection() == SelectionType::Single {
                ids.truncate(1);
                if ids.is_empty() && group.required {
                    if let Some(id) = default_or_first(group) {
                        ids.push(id);
                    }
                }
            }
            ids
        }
        None => defaults(group),
    }
}

fn defaults(group: &FeatureGroup) -> Vec<String> {
    if group.selection() == SelectionType::Single {
        if let Some(option) = group.options.iter().find(|option| option.default_enabled) {
            vec![option.id.clone()]
        } else if group.required {
            group
                .options
                .first()
                .map(|option| vec![option.id.clone()])
                .unwrap_or_default()
        } else {
            Vec::new()
        }
    } else {
        group
            .options
            .iter()
            .filter(|option| option.default_enabled)
            .map(|option| option.id.clone())
            .collect()
    }
}

fn default_or_first(group: &FeatureGroup) -> Option<String> {
    group
        .options
        .iter()
        .find(|option| option.default_enabled)
        .or_else(|| group.options.first())
        .map(|option| option.id.clone())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::proto::core::v1::FeatureOption;

    fn option(id: &str, default: bool, files: &[&str]) -> FeatureOption {
        FeatureOption {
            id: id.into(),
            default_enabled: default,
            files: files.iter().map(|f| f.to_string()).collect(),
            ..Default::default()
        }
    }

    fn group(
        id: &str,
        selection: SelectionType,
        required: bool,
        options: Vec<FeatureOption>,
    ) -> FeatureGroup {
        FeatureGroup {
            id: id.into(),
            selection: selection as i32,
            required,
            options,
            ..Default::default()
        }
    }

    fn sel(pairs: &[(&str, &[&str])]) -> FeatureSelection {
        let mut selected = BTreeMap::new();
        for (addr, ids) in pairs {
            selected.insert(
                addr.to_string(),
                ids.iter().map(|s| s.to_string()).collect(),
            );
        }
        FeatureSelection { selected }
    }

    #[test]
    fn defaults_when_untouched() {
        let model = Some(FeatureModel {
            groups: vec![group(
                "g",
                SelectionType::Single,
                false,
                vec![
                    option("a", true, &["a.jar"]),
                    option("b", false, &["b.jar"]),
                ],
            )],
        });
        let (_, files) = resolve_active(&model, &FeatureSelection::default());
        assert_eq!(files, HashSet::from(["a.jar".to_string()]));
    }

    #[test]
    fn single_picks_one_multi_keeps_all() {
        let model = Some(FeatureModel {
            groups: vec![
                group(
                    "s",
                    SelectionType::Single,
                    false,
                    vec![option("a", false, &["a"]), option("b", false, &["b"])],
                ),
                group(
                    "m",
                    SelectionType::Multi,
                    false,
                    vec![option("x", false, &["x"]), option("y", false, &["y"])],
                ),
            ],
        });
        let (_, files) = resolve_active(&model, &sel(&[("s", &["a", "b"]), ("m", &["x", "y"])]));
        assert_eq!(
            files,
            HashSet::from(["a".to_string(), "x".to_string(), "y".to_string()])
        );
    }

    #[test]
    fn presence_empty_clears_defaults() {
        let model = Some(FeatureModel {
            groups: vec![group(
                "m",
                SelectionType::Multi,
                false,
                vec![option("a", true, &["a"])],
            )],
        });
        let (_, files) = resolve_active(&model, &sel(&[("m", &[])]));
        assert!(files.is_empty());
    }

    #[test]
    fn required_single_refills_when_cleared() {
        let model = Some(FeatureModel {
            groups: vec![group(
                "g",
                SelectionType::Single,
                true,
                vec![option("a", true, &["a"]), option("b", false, &["b"])],
            )],
        });
        let (_, files) = resolve_active(&model, &sel(&[("g", &[])]));
        assert_eq!(files, HashSet::from(["a".to_string()]));
    }

    #[test]
    fn stale_ids_self_heal() {
        let model = Some(FeatureModel {
            groups: vec![group(
                "m",
                SelectionType::Multi,
                false,
                vec![option("a", false, &["a"])],
            )],
        });
        let (_, files) = resolve_active(&model, &sel(&[("m", &["a", "ghost"])]));
        assert_eq!(files, HashSet::from(["a".to_string()]));
    }

    #[test]
    fn nesting_descends_only_into_chosen() {
        let sub = group(
            "extra",
            SelectionType::Multi,
            false,
            vec![option("e", false, &["e.jar"])],
        );
        let mut sodium = option("sodium", true, &["sodium.jar"]);
        sodium.groups = vec![sub];
        let model = Some(FeatureModel {
            groups: vec![group(
                "g",
                SelectionType::Single,
                false,
                vec![sodium, option("other", false, &["other.jar"])],
            )],
        });
        let (_, files) = resolve_active(&model, &sel(&[("g#sodium/extra", &["e"])]));
        assert_eq!(
            files,
            HashSet::from(["sodium.jar".to_string(), "e.jar".to_string()])
        );

        let (_, files_off) = resolve_active(
            &model,
            &sel(&[("g", &["other"]), ("g#sodium/extra", &["e"])]),
        );
        assert_eq!(files_off, HashSet::from(["other.jar".to_string()]));
    }

    #[test]
    fn shared_file_kept_while_any_owner_active() {
        let model = Some(FeatureModel {
            groups: vec![group(
                "m",
                SelectionType::Multi,
                false,
                vec![
                    option("a", false, &["shared.jar", "a.jar"]),
                    option("b", false, &["shared.jar", "b.jar"]),
                ],
            )],
        });
        let (_, files) = resolve_active(&model, &sel(&[("m", &["a"])]));
        assert!(files.contains("shared.jar"));
        assert!(files.contains("a.jar"));
        assert!(!files.contains("b.jar"));
    }

    fn with_meta(
        mut option: FeatureOption,
        requires: &[&str],
        incompatible: &[&str],
    ) -> FeatureOption {
        option.meta = Some(crate::proto::core::v1::OptionMeta {
            requires: requires.iter().map(|s| s.to_string()).collect(),
            incompatible_with: incompatible.iter().map(|s| s.to_string()).collect(),
            ..Default::default()
        });
        option
    }

    #[test]
    fn requires_drops_option_when_dependency_inactive() {
        let model = Some(FeatureModel {
            groups: vec![
                group(
                    "base",
                    SelectionType::Multi,
                    false,
                    vec![option("core", false, &["core.jar"])],
                ),
                group(
                    "addons",
                    SelectionType::Multi,
                    false,
                    vec![with_meta(
                        option("addon", true, &["addon.jar"]),
                        &["base#core"],
                        &[],
                    )],
                ),
            ],
        });

        let (_, without) = resolve_active(&model, &FeatureSelection::default());
        assert!(
            without.is_empty(),
            "addon must drop while its requirement is inactive: {without:?}"
        );

        let (_, with) = resolve_active(&model, &sel(&[("base", &["core"])]));
        assert_eq!(
            with,
            HashSet::from(["core.jar".to_string(), "addon.jar".to_string()])
        );
    }

    #[test]
    fn incompatible_drops_the_later_declared() {
        let model = Some(FeatureModel {
            groups: vec![group(
                "m",
                SelectionType::Multi,
                false,
                vec![
                    option("first", true, &["first.jar"]),
                    with_meta(option("second", true, &["second.jar"]), &[], &["m#first"]),
                ],
            )],
        });
        let (_, files) = resolve_active(&model, &FeatureSelection::default());
        assert_eq!(files, HashSet::from(["first.jar".to_string()]));
    }

    #[test]
    fn dropping_a_parent_cascades_to_nested_descendants() {
        let sub = group(
            "extras",
            SelectionType::Multi,
            false,
            vec![option("extra", true, &["extra.jar"])],
        );
        let mut parent = with_meta(
            option("parent", true, &["parent.jar"]),
            &["missing#gone"],
            &[],
        );
        parent.groups = vec![sub];
        let model = Some(FeatureModel {
            groups: vec![group("g", SelectionType::Multi, false, vec![parent])],
        });
        let (addrs, files) = resolve_active(&model, &FeatureSelection::default());
        assert!(
            files.is_empty(),
            "cascade must drop nested files too: {files:?}"
        );
        assert!(
            addrs.is_empty(),
            "cascade must drop nested addresses too: {addrs:?}"
        );
    }

    #[test]
    fn mutual_incompatibility_terminates() {
        let model = Some(FeatureModel {
            groups: vec![group(
                "m",
                SelectionType::Multi,
                false,
                vec![
                    with_meta(option("a", true, &["a.jar"]), &[], &["m#b"]),
                    with_meta(option("b", true, &["b.jar"]), &[], &["m#a"]),
                ],
            )],
        });
        let (_, files) = resolve_active(&model, &FeatureSelection::default());
        assert_eq!(files, HashSet::from(["a.jar".to_string()]));
    }

    #[test]
    fn optional_paths_covers_whole_tree() {
        let sub = group(
            "extra",
            SelectionType::Multi,
            false,
            vec![option("e", false, &["e.jar"])],
        );
        let mut sodium = option("sodium", false, &["sodium.jar"]);
        sodium.groups = vec![sub];
        let model = Some(FeatureModel {
            groups: vec![group("g", SelectionType::Single, false, vec![sodium])],
        });
        let optional = optional_paths(&model);
        assert_eq!(
            optional,
            HashSet::from(["sodium.jar".to_string(), "e.jar".to_string()])
        );
    }

    #[test]
    fn chosen_options_add_launch_arguments_in_order() {
        let mut shaders = option("shaders", true, &["mods/iris.jar"]);
        shaders.jvm_args = vec!["-Diris.enable=true".into()];
        shaders.classpath = vec!["mods/iris.jar".into()];
        let mut zoom = option("zoom", true, &["mods/zoom.jar"]);
        zoom.game_args = vec!["--zoom".into()];
        let plain = option("plain", false, &["mods/plain.jar"]);

        let model = Some(FeatureModel {
            groups: vec![group(
                "graphics",
                SelectionType::Multi,
                false,
                vec![shaders, zoom, plain],
            )],
        });

        let extras = resolve_extras(&model, &sel(&[("graphics", &["shaders", "zoom", "plain"])]));
        assert_eq!(extras.jvm_args, vec!["-Diris.enable=true"]);
        assert_eq!(extras.game_args, vec!["--zoom"]);
        assert_eq!(extras.classpath, vec!["mods/iris.jar"]);
    }

    #[test]
    fn arguments_follow_the_order_of_the_options_not_their_ids() {
        let mut zebra = option("zebra", true, &["mods/zebra.jar"]);
        zebra.jvm_args = vec!["-Dzebra=1".into(), "-Dzebra=2".into()];
        zebra.game_args = vec!["--zebra".into()];
        zebra.classpath = vec!["mods/zebra.jar".into()];
        let mut alpha = option("alpha", true, &["mods/alpha.jar"]);
        alpha.jvm_args = vec!["-Dalpha=1".into(), "-Dalpha=2".into()];
        alpha.game_args = vec!["--alpha".into()];
        alpha.classpath = vec!["mods/alpha.jar".into()];

        let model = Some(FeatureModel {
            groups: vec![group(
                "graphics",
                SelectionType::Multi,
                false,
                vec![zebra, alpha],
            )],
        });

        let extras = resolve_extras(&model, &sel(&[("graphics", &["alpha", "zebra"])]));
        assert_eq!(
            extras.jvm_args,
            vec!["-Dzebra=1", "-Dzebra=2", "-Dalpha=1", "-Dalpha=2"]
        );
        assert_eq!(extras.game_args, vec!["--zebra", "--alpha"]);
        assert_eq!(extras.classpath, vec!["mods/zebra.jar", "mods/alpha.jar"]);
    }

    #[test]
    fn a_classpath_entry_that_escapes_the_profile_is_dropped() {
        let mut cheat = option("cheat", true, &["mods/cheat.jar"]);
        cheat.classpath = vec![
            "/etc/passwd".into(),
            "../../escape.jar".into(),
            ".laminara/state.jar".into(),
            "mods/cheat.jar".into(),
        ];

        let model = Some(FeatureModel {
            groups: vec![group("graphics", SelectionType::Multi, false, vec![cheat])],
        });

        let extras = resolve_extras(&model, &sel(&[("graphics", &["cheat"])]));
        assert_eq!(extras.classpath, vec!["mods/cheat.jar"]);
    }

    #[test]
    fn arguments_of_a_switched_off_option_do_not_leak() {
        let mut shaders = option("shaders", false, &["mods/iris.jar"]);
        shaders.jvm_args = vec!["-Diris.enable=true".into()];
        let model = Some(FeatureModel {
            groups: vec![group(
                "graphics",
                SelectionType::Multi,
                false,
                vec![shaders],
            )],
        });

        let extras = resolve_extras(&model, &sel(&[("graphics", &[])]));
        assert!(
            extras.jvm_args.is_empty(),
            "a switched off option must not touch the launch command"
        );
    }

    #[test]
    fn option_dropped_by_a_constraint_takes_its_arguments_with_it() {
        let mut needs_base = option("extra", true, &["mods/extra.jar"]);
        needs_base.jvm_args = vec!["-Dextra=1".into()];
        needs_base.meta = Some(crate::proto::core::v1::OptionMeta {
            requires: vec!["graphics#base".into()],
            ..Default::default()
        });
        let model = Some(FeatureModel {
            groups: vec![group(
                "graphics",
                SelectionType::Multi,
                false,
                vec![needs_base],
            )],
        });

        let extras = resolve_extras(&model, &sel(&[("graphics", &["extra"])]));
        assert!(
            extras.jvm_args.is_empty(),
            "an option dropped by a constraint takes its arguments with it"
        );
    }
}

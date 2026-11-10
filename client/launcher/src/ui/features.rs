use std::collections::HashSet;

use laminara_core::features::{self, FeatureSelection, nested_group_addr, option_addr};
use laminara_core::proto::core::v1::{FeatureGroup, FeatureModel, SelectionType};

use crate::FeatureRow;

const GROUP_GAP: f32 = 24.0;
const HEADER_GAP: f32 = 8.0;
const OPTION_GAP: f32 = 6.0;
const NESTED_GAP: f32 = 12.0;

pub struct FeatureEditor {
    pub profile: String,
    model: Option<FeatureModel>,
    pub selection: FeatureSelection,
}

impl FeatureEditor {
    pub fn new(profile: String, model: Option<FeatureModel>, selection: FeatureSelection) -> Self {
        FeatureEditor {
            profile,
            model,
            selection,
        }
    }

    pub fn is_empty(&self) -> bool {
        self.model
            .as_ref()
            .is_none_or(|model| model.groups.is_empty())
    }

    pub fn pick(&mut self, group: &str, option: &str, active: bool, single: bool) {
        let ids = if single {
            vec![option.to_string()]
        } else {
            let mut chosen = self.chosen(group);
            if active {
                chosen.retain(|id| id != option);
            } else {
                chosen.push(option.to_string());
            }
            chosen
        };
        self.selection.selected.insert(group.to_string(), ids);
    }

    pub fn clear(&mut self, group: &str) {
        self.selection
            .selected
            .insert(group.to_string(), Vec::new());
    }

    pub fn reset(&mut self) {
        self.selection = FeatureSelection::default();
    }

    fn chosen(&self, addr: &str) -> Vec<String> {
        let Some(model) = &self.model else {
            return Vec::new();
        };
        find_group(&model.groups, "", addr)
            .map(|group| features::effective(group, addr, &self.selection))
            .unwrap_or_default()
    }

    pub fn rows(&self) -> Vec<FeatureRow> {
        let mut rows = Vec::new();
        let Some(model) = &self.model else {
            return rows;
        };
        let (active, _) = features::resolve_active(&self.model, &self.selection);
        for (index, group) in model.groups.iter().enumerate() {
            let gap = if index == 0 { 0.0 } else { GROUP_GAP };
            walk(group, group.id.clone(), 0, gap, &active, &mut rows);
        }
        rows
    }
}

fn find_group<'a>(
    groups: &'a [FeatureGroup],
    parent: &str,
    addr: &str,
) -> Option<&'a FeatureGroup> {
    for group in groups {
        let group_addr = if parent.is_empty() {
            group.id.clone()
        } else {
            nested_group_addr(parent, &group.id)
        };
        if group_addr == addr {
            return Some(group);
        }
        for option in &group.options {
            let option_addr = option_addr(&group_addr, &option.id);
            if let Some(found) = find_group(&option.groups, &option_addr, addr) {
                return Some(found);
            }
        }
    }
    None
}

fn walk(
    group: &FeatureGroup,
    addr: String,
    depth: i32,
    gap: f32,
    active: &HashSet<String>,
    rows: &mut Vec<FeatureRow>,
) {
    let single = group.selection() == SelectionType::Single;
    rows.push(FeatureRow {
        header: true,
        depth,
        gap,
        group: addr.clone().into(),
        title: group.title.clone().into(),
        description: group.description.clone().into(),
        opens: depth > 0,
        ..FeatureRow::default()
    });
    let mut next_gap = HEADER_GAP;
    if single && !group.required {
        let none_active = !group
            .options
            .iter()
            .any(|option| active.contains(&option_addr(&addr, &option.id)));
        rows.push(FeatureRow {
            depth,
            gap: next_gap,
            group: addr.clone().into(),
            title: "Нет".into(),
            active: none_active,
            single: true,
            none: true,
            ..FeatureRow::default()
        });
        next_gap = OPTION_GAP;
    }
    for option in &group.options {
        let option_addr = option_addr(&addr, &option.id);
        let is_active = active.contains(&option_addr);
        let meta = option.meta.as_ref();
        let blocked = if is_active {
            ""
        } else {
            blocked_reason(
                meta.map(|meta| meta.requires.as_slice())
                    .unwrap_or_default(),
                meta.map(|meta| meta.incompatible_with.as_slice())
                    .unwrap_or_default(),
                active,
            )
        };
        rows.push(FeatureRow {
            depth,
            gap: next_gap,
            group: addr.clone().into(),
            option: option.id.clone().into(),
            title: option.title.clone().into(),
            description: option.description.clone().into(),
            badge: meta
                .map(|meta| meta.badge.clone())
                .unwrap_or_default()
                .into(),
            active: is_active,
            single,
            blocked: blocked.into(),
            ..FeatureRow::default()
        });
        next_gap = OPTION_GAP;
        if is_active {
            for sub in &option.groups {
                walk(
                    sub,
                    nested_group_addr(&option_addr, &sub.id),
                    depth + 1,
                    NESTED_GAP,
                    active,
                    rows,
                );
            }
        }
    }
}

fn blocked_reason(
    requires: &[String],
    incompatible: &[String],
    active: &HashSet<String>,
) -> &'static str {
    if requires.iter().any(|addr| !active.contains(addr)) {
        "Требует другую опцию"
    } else if incompatible.iter().any(|addr| active.contains(addr)) {
        "Несовместимо с выбранным"
    } else {
        ""
    }
}

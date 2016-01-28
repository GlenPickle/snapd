// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package skills

import (
	"fmt"
	"sort"
	"sync"

	"github.com/ubuntu-core/snappy/snap"
)

// Repository stores all known snappy skills and slots and types.
type Repository struct {
	// Protects the internals from concurrent access.
	m     sync.Mutex
	types map[string]Type
	// Indexed by [snapName][skillName]
	skills          map[string]map[string]*Skill
	slots           map[string]map[string]*Slot
	skillUsedBySlot map[*Slot]map[*Skill]bool
	slotsUsingSkill map[*Skill]map[*Slot]bool
}

// NewRepository creates an empty skill repository.
func NewRepository() *Repository {
	return &Repository{
		types:           make(map[string]Type),
		skills:          make(map[string]map[string]*Skill),
		slots:           make(map[string]map[string]*Slot),
		skillUsedBySlot: make(map[*Slot]map[*Skill]bool),
		slotsUsingSkill: make(map[*Skill]map[*Slot]bool),
	}
}

// Type returns a type with a given name.
func (r *Repository) Type(typeName string) Type {
	r.m.Lock()
	defer r.m.Unlock()

	return r.types[typeName]
}

// AddType adds the provided skill type to the repository.
func (r *Repository) AddType(t Type) error {
	r.m.Lock()
	defer r.m.Unlock()

	typeName := t.Name()
	if err := ValidateName(typeName); err != nil {
		return err
	}
	if _, ok := r.types[typeName]; ok {
		return fmt.Errorf("cannot add skill type: %q, type name is in use", typeName)
	}
	r.types[typeName] = t
	return nil
}

// AllSkills returns all skills of the given type.
// If skillType is the empty string, all skills are returned.
func (r *Repository) AllSkills(skillType string) []*Skill {
	r.m.Lock()
	defer r.m.Unlock()

	var result []*Skill
	for _, skillsForSnap := range r.skills {
		for _, skill := range skillsForSnap {
			if skillType == "" || skill.Type == skillType {
				result = append(result, skill)
			}
		}
	}
	sort.Sort(bySkillSnapAndName(result))
	return result
}

// Skills returns the skills offered by the named snap.
func (r *Repository) Skills(snapName string) []*Skill {
	r.m.Lock()
	defer r.m.Unlock()

	var result []*Skill
	for _, skill := range r.skills[snapName] {
		result = append(result, skill)
	}
	sort.Sort(bySkillSnapAndName(result))
	return result
}

// Skill returns the specified skill from the named snap.
func (r *Repository) Skill(snapName, skillName string) *Skill {
	r.m.Lock()
	defer r.m.Unlock()

	return r.skills[snapName][skillName]
}

// AddSkill adds a skill to the repository.
// Skill names must be valid snap names, as defined by ValidateName.
// Skill name must be unique within a particular snap.
func (r *Repository) AddSkill(skill *Skill) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Reject snaps with invalid names
	if err := snap.ValidateName(skill.Snap); err != nil {
		return err
	}
	// Reject skill with invalid names
	if err := ValidateName(skill.Name); err != nil {
		return err
	}
	t := r.types[skill.Type]
	if t == nil {
		return fmt.Errorf("cannot add skill, skill type %q is not known", skill.Type)
	}
	// Reject skill that don't pass type-specific sanitization
	if err := t.Sanitize(skill); err != nil {
		return err
	}
	if _, ok := r.skills[skill.Snap][skill.Name]; ok {
		return fmt.Errorf("cannot add skill, skill name %q is in use", skill.Name)
	}
	if r.skills[skill.Snap] == nil {
		r.skills[skill.Snap] = make(map[string]*Skill)
	}
	r.skills[skill.Snap][skill.Name] = skill
	return nil
}

// RemoveSkill removes the named skill provided by a given snap.
// The removed skill must exist and must not be used anywhere.
func (r *Repository) RemoveSkill(snapName, skillName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Ensure that such skill exists
	skill := r.skills[snapName][skillName]
	if skill == nil {
		return fmt.Errorf("cannot remove skill, skill %q:%q does not exist", snapName, skillName)
	}
	// Ensure that the skill is not used by any slot
	if len(r.slotsUsingSkill[skill]) > 0 {
		return fmt.Errorf("cannot remove skill, skill %q:%q is used by at least one slot", snapName, skillName)
	}
	delete(r.skills[snapName], skillName)
	return nil
}

// AllSlots returns all skill slots of the given type.
// If skillType is the empty string, all skill slots are returned.
func (r *Repository) AllSlots(skillType string) []*Slot {
	r.m.Lock()
	defer r.m.Unlock()

	var result []*Slot
	for _, slotsForSnap := range r.slots {
		for _, slot := range slotsForSnap {
			if skillType == "" || slot.Type == skillType {
				result = append(result, slot)
			}
		}
	}
	sort.Sort(bySlotSnapAndName(result))
	return result
}

// Slots returns the skill slots offered by the named snap.
func (r *Repository) Slots(snapName string) []*Slot {
	r.m.Lock()
	defer r.m.Unlock()

	var result []*Slot
	for _, slot := range r.slots[snapName] {
		result = append(result, slot)
	}
	sort.Sort(bySlotSnapAndName(result))
	return result
}

// Slot returns the specified skill slot from the named snap.
func (r *Repository) Slot(snapName, slotName string) *Slot {
	r.m.Lock()
	defer r.m.Unlock()

	return r.slots[snapName][slotName]
}

// AddSlot adds a new slot to the repository.
// Adding a slot with invalid name returns an error.
// Adding a slot that has the same name and snap name as another slot returns an error.
func (r *Repository) AddSlot(slot *Slot) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Reject skill with invalid names
	if err := ValidateName(slot.Name); err != nil {
		return err
	}
	// TODO: ensure the snap is correct
	// TODO: ensure that apps are correct
	t := r.types[slot.Type]
	if t == nil {
		return fmt.Errorf("cannot add slot, skill type %q is not known", slot.Type)
	}
	if _, ok := r.slots[slot.Snap][slot.Name]; ok {
		return fmt.Errorf("cannot add slot, slot name %q is in use", slot.Name)
	}
	if r.slots[slot.Snap] == nil {
		r.slots[slot.Snap] = make(map[string]*Slot)
	}
	r.slots[slot.Snap][slot.Name] = slot
	return nil
}

// RemoveSlot removes a named slot from the given snap.
// Removing a slot that doesn't exist returns an error.
// Removing a slot that uses a skill returns an error.
func (r *Repository) RemoveSlot(snapName, slotName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Ensure that such slot exists
	slot := r.slots[snapName][slotName]
	if slot == nil {
		return fmt.Errorf("cannot remove slot, slot %q:%q does not exist", snapName, slotName)
	}
	// Ensure that the slot is not using any skills
	if len(r.skillUsedBySlot[slot]) > 0 {
		return fmt.Errorf("cannot remove slot, slot %q:%q uses at least one skill", snapName, slotName)
	}
	delete(r.slots[snapName], slotName)
	return nil
}

// Grant grants the named skill to the named slot of the given snap.
// The skill and the slot must have the same type.
func (r *Repository) Grant(skillSnapName, skillName, slotSnapName, slotName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Ensure that such skill exists
	skill := r.skills[skillSnapName][skillName]
	if skill == nil {
		return fmt.Errorf("cannot grant skill, no such skill %q:%q", skillSnapName, skillName)
	}
	// Ensure that such slot exists
	slot := r.slots[slotSnapName][slotName]
	if slot == nil {
		return fmt.Errorf("cannot grant skill, no such slot %q:%q", slotSnapName, slotName)
	}
	// Ensure that skill and slot are compatible
	if slot.Type != skill.Type {
		return fmt.Errorf("cannot grant skill, skill type %q doesn't match slot type %q", skill.Type, slot.Type)
	}
	// Ensure that slot and skill are not connected yet
	if r.skillUsedBySlot[slot][skill] {
		return fmt.Errorf("cannot grant skill, skill %q:%q is already used by slot %q:%q",
			skill.Snap, skill.Name, slot.Snap, slot.Name)
	}
	// Grant the skill
	if r.skillUsedBySlot[slot] == nil {
		r.skillUsedBySlot[slot] = make(map[*Skill]bool)
	}
	if r.slotsUsingSkill[skill] == nil {
		r.slotsUsingSkill[skill] = make(map[*Slot]bool)
	}
	r.skillUsedBySlot[slot][skill] = true
	r.slotsUsingSkill[skill][slot] = true
	return nil
}

// Revoke revokes the named skill from the slot of the given snap.
func (r *Repository) Revoke(skillSnapName, skillName, slotSnapName, slotName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Ensure that such skill exists
	skill := r.skills[skillSnapName][skillName]
	if skill == nil {
		return fmt.Errorf("cannot revoke skill, no such skill %q:%q", skillSnapName, skillName)
	}
	// Ensure that such slot exists
	slot := r.slots[slotSnapName][slotName]
	if slot == nil {
		return fmt.Errorf("cannot revoke skill, no such slot %q:%q", slotSnapName, slotName)
	}
	// Ensure that slot and skill are connected
	if !r.skillUsedBySlot[slot][skill] {
		return fmt.Errorf("cannot revoke skill, skill %q:%q is not used by slot %q:%q",
			skill.Snap, skill.Name, slot.Snap, slot.Name)
	}
	delete(r.skillUsedBySlot[slot], skill)
	delete(r.slotsUsingSkill[skill], slot)
	return nil
}

// GrantedTo returns all the skills granted to a given snap.
func (r *Repository) GrantedTo(snapName string) map[*Slot][]*Skill {
	r.m.Lock()
	defer r.m.Unlock()

	result := make(map[*Slot][]*Skill)
	for _, slot := range r.slots[snapName] {
		for skill := range r.skillUsedBySlot[slot] {
			result[slot] = append(result[slot], skill)
		}
		sort.Sort(bySkillSnapAndName(result[slot]))
	}
	return result
}

// GrantedBy returns all of the skills granted by a given snap.
func (r *Repository) GrantedBy(snapName string) map[*Skill][]*Slot {
	r.m.Lock()
	defer r.m.Unlock()

	result := make(map[*Skill][]*Slot)
	for _, skill := range r.skills[snapName] {
		for slot := range r.slotsUsingSkill[skill] {
			result[skill] = append(result[skill], slot)
		}
		sort.Sort(bySlotSnapAndName(result[skill]))
	}
	return result
}

// Support for sort.Interface

type bySkillSnapAndName []*Skill

func (c bySkillSnapAndName) Len() int      { return len(c) }
func (c bySkillSnapAndName) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c bySkillSnapAndName) Less(i, j int) bool {
	if c[i].Snap != c[j].Snap {
		return c[i].Snap < c[j].Snap
	}
	return c[i].Name < c[j].Name
}

type bySlotSnapAndName []*Slot

func (c bySlotSnapAndName) Len() int      { return len(c) }
func (c bySlotSnapAndName) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c bySlotSnapAndName) Less(i, j int) bool {
	if c[i].Snap != c[j].Snap {
		return c[i].Snap < c[j].Snap
	}
	return c[i].Name < c[j].Name
}

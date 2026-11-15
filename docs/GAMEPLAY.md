# Protoverse — How to play

## The loop

The world advances in **ticks** (every 10 seconds). You give orders at any time; each order resolves on a tick, in the order it came due. Your home planet and any planets you've colonized pay you **credits** every tick. Spend credits on ships, use ships to claim more planets and to fight, and watch your score climb on the leaderboard.

## Starting out

Signing up gives you:

- **500 credits**
- a **home planet** (20 credits/tick, a huge deposit) at a random spot on the 100×100 map
- a **starting fleet** of 2 scouts and 1 fighter parked on it

## Ships

| Ship | Cost | Attack | Defense | HP | Speed (units/tick) | Build time (ticks) | Role |
|---|---|---|---|---|---|---|---|
| scout | 50 | 2 | 1 | 20 | 20 | 1 | Fast and cheap; explores and runs errands |
| fighter | 150 | 8 | 3 | 40 | 12 | 2 | Balanced workhorse |
| cruiser | 500 | 20 | 10 | 120 | 8 | 4 | Slow, tough, hard-hitting |
| colony | 800 | 0 | 2 | 60 | 6 | 5 | Claims a neutral planet, consumed doing so |
| trooper | 300 | 1 | 1 | 30 | 10 | 3 | Marines: captures an enemy planet whose shield is down, consumed doing so |

(`GetCatalog` / `pvctl catalog` always shows the live numbers.)

## Orders

**Build** — at a planet you own. Credits are charged immediately; the ships appear (joining a fleet parked there, or forming a new one) when construction finishes. If you lose the planet before then, you're refunded.

**Move** — a fleet travels at the speed of its *slowest* ship, so the trip takes `ceil(distance / speed)` ticks. A fleet that's travelling can't be given another move, and is shown part-way along its route to everyone.

**Attack** — your fleet vs an enemy fleet **within 10 units** at the moment the attack resolves (you get early feedback if it's out of range when you order it, but fleets keep moving, so it's re-checked). Damage is your fleet's total attack minus their total defense (±10%), spent against their ships in order. A surviving defender **returns fire** with the same formula. The winner **salvages 50%** of the cost of every ship destroyed. A fleet can only have one attack queued at a time.

**Colonize** — a fleet containing a colony ship, within 5 units of an *unowned* planet. The colony ship is consumed and the planet becomes yours; from the next tick it pays out its production rate until its deposit runs dry.

**Invade** — your fleet vs an enemy **planet** within 10 units. See *Planets under siege* below.

**Fortify** — spend credits to raise one of your planets by one defense level (cost `200 × (current level + 1)`, takes 2 ticks). It adds shield immediately when it completes.

## Planets under siege

Every planet has a **defense level** (0–10) and a **shield**. Neutral planets are level 0; homeworlds start at level 2. Higher levels mean more armour, firepower and shield:

| | formula | level 0 | level 2 (homeworld) | level 10 |
|---|---|---|---|---|
| max shield | 50 + 40 × level | 50 | 130 | 450 |
| armour (subtracted from your attack) | 3 + 2 × level | 3 | 7 | 23 |
| firepower (against your fleet) | 10 + 8 × level | 10 | 26 | 90 |

An **invasion** is one exchange each tick you order it:

1. Your fleet's total attack, minus the planet's armour (±10%), drains the planet's **shield**.
2. The planet fires back at your fleet, using your fleet's total defense as armour. If its guns destroy ships, the planet's owner **salvages** half their cost.
3. If the shield hit **0** and your fleet still has a **trooper**, the trooper is consumed and the planet is **captured**: it keeps one fewer defense level and starts at a quarter of its new maximum shield — so expect a counter-attack.

Without a trooper you can still bombard a planet's shield, but you can't take it. Shields **regenerate 5 per tick**, so an assault has to be concentrated: bring enough firepower to breach the shield in one or two volleys, plus a trooper, and enough armour to survive the return fire. Fortifying makes this far more expensive for attackers, which is what makes it worth the credits.

**Sanctuary rule:** a commander's *last* planet cannot be invaded, so nobody can be wiped off the map and everyone can rebuild. A fleet can have only one invasion queued at a time, and a planet only one fortification.

## Economy

Each planet has a **production** rate (credits/tick) and a finite **deposit**. Every tick an owned planet converts up to its production out of its deposit into your credits. Neutral planets have 2,000–10,000 deposits and produce 5–25/tick — worth taking early. Your home planet's deposit is effectively bottomless.

## Score

```
100 per planet owned
 10 per ship alive
 50 per enemy fleet destroyed (including by return fire)
 75 per enemy planet captured
  1 per 10 damage dealt
  1 per 100 credits banked
```

## A first ten minutes

1. `pvctl map` — find neutral planets near your home (`o` on the map).
2. Build a colony ship (800 credits). You start with 500, so wait ~15 ticks of income, or sell the idea: build a couple of scouts first (50 each) to scout the map.
3. Move your fleet next to a neutral planet (within 5 units), then `pvctl colonize <fleet> <planet>`.
4. Keep an eye on `pvctl watch` — if an enemy fleet turns up within 10 units of one of yours, you'll see combat events.
5. Spend surplus credits on fighters and cruisers; a big fleet next to a rival's lone scouts is a salvage machine.

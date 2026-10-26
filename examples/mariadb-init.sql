-- Deterministic MariaDB Signals test-environment seed (MariaDB collection PR2).
--
-- Applied by scripts/mariadb-dev-fixture.sh against the pinned official MariaDB
-- Dev Image. Everything here is version-controlled and deterministic: no
-- interactive /tmp SQL, no hand-trimming, no developer-workspace dependence.
--
-- Read-only monitoring user + the EXACT minimum grants per
-- specifications/mariadb-grants.md: SELECT (read every information_schema /
-- performance_schema view the catalog uses) + PROCESS (so
-- mariadb_processlist_v1 surfaces all sessions, not just the monitor's own).
-- No elevated/optional grant is needed for the MariaDB catalog.
--
-- The schema/seed/workload exist ONLY to exercise the Signals collectors
-- (table_statistics, processlist, innodb_metrics, …). It is deliberately NOT
-- shaped to trigger any Analyzer rule/finding — Analyzer is downstream.

CREATE USER IF NOT EXISTS 'signals_ro'@'%' IDENTIFIED BY 'signals_ro_pw';
GRANT SELECT, PROCESS ON *.* TO 'signals_ro'@'%';
FLUSH PRIVILEGES;

CREATE DATABASE IF NOT EXISTS signals_fixture;
USE signals_fixture;

-- InnoDB tables so TABLE_STATISTICS + INNODB_METRICS have real rows.
CREATE TABLE IF NOT EXISTS accounts (
  id BIGINT PRIMARY KEY,
  region VARCHAR(16),
  balance DECIMAL(12,2),
  updated_at DATETIME,
  KEY region_idx (region)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS ledger (
  id BIGINT PRIMARY KEY,
  account_id BIGINT,
  amount DECIMAL(12,2),
  entry_at DATETIME,
  KEY account_idx (account_id)
) ENGINE=InnoDB;

-- #538 Replay Tier-1 evidence fixture: a representative table that exercises the
-- full columns + index-inventory contract so the semantic qualification can
-- assert the evidence survives collection -> persistence -> export:
--   * a nullable column (note) and NOT NULL columns (id, tenant_id, created_at,
--     email, status);
--   * a primary key (id);
--   * an ordinary single-column index (idx_status);
--   * a UNIQUE index (uq_email);
--   * a composite index with DELIBERATELY observable column ordering
--     (idx_tenant_created = tenant_id, created_at) — the exported evidence MUST
--     preserve that exact (tenant_id, created_at) order.
CREATE TABLE IF NOT EXISTS replay_fixture (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  created_at DATETIME NOT NULL,
  email VARCHAR(255) NOT NULL,
  note VARCHAR(255) NULL,
  status INT NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_email (email),
  KEY idx_status (status),
  KEY idx_tenant_created (tenant_id, created_at)
) ENGINE=InnoDB;

INSERT IGNORE INTO replay_fixture (id, tenant_id, created_at, email, note, status) VALUES
 (1,100,NOW(),'a@example.com','n1',1),(2,100,NOW(),'b@example.com',NULL,0),
 (3,200,NOW(),'c@example.com','n3',1),(4,200,NOW(),'d@example.com',NULL,0),
 (5,300,NOW(),'e@example.com','n5',1);

INSERT IGNORE INTO accounts (id, region, balance, updated_at) VALUES
 (1,'eu',100.00,NOW()),(2,'us',200.00,NOW()),(3,'eu',300.00,NOW()),(4,'ap',400.00,NOW()),
 (5,'us',500.00,NOW()),(6,'eu',600.00,NOW()),(7,'ap',700.00,NOW()),(8,'us',800.00,NOW()),
 (9,'eu',900.00,NOW()),(10,'ap',1000.00,NOW()),(11,'us',110.00,NOW()),(12,'eu',120.00,NOW()),
 (13,'ap',130.00,NOW()),(14,'us',140.00,NOW()),(15,'eu',150.00,NOW()),(16,'ap',160.00,NOW()),
 (17,'us',170.00,NOW()),(18,'eu',180.00,NOW()),(19,'ap',190.00,NOW()),(20,'us',200.00,NOW());

INSERT IGNORE INTO ledger (id, account_id, amount, entry_at) VALUES
 (1,1,10.00,NOW()),(2,2,20.00,NOW()),(3,3,30.00,NOW()),(4,4,40.00,NOW()),(5,5,50.00,NOW()),
 (6,6,60.00,NOW()),(7,7,70.00,NOW()),(8,8,80.00,NOW()),(9,9,90.00,NOW()),(10,10,100.00,NOW()),
 (11,11,110.00,NOW()),(12,12,120.00,NOW()),(13,13,130.00,NOW()),(14,14,140.00,NOW()),(15,15,150.00,NOW());

-- Deterministic workload so the collectors read a warmed server.
SELECT region, COUNT(*) AS n, SUM(balance) AS total FROM accounts GROUP BY region ORDER BY total DESC;
SELECT a.region, COUNT(l.id) AS entries FROM accounts a JOIN ledger l ON l.account_id = a.id GROUP BY a.region;
SELECT COUNT(*) AS big FROM ledger WHERE amount > 50;

-- #647: MariaDB needs the statements-digest consumer + statement instruments
-- explicitly enabled (performance_schema=ON is passed at server start) so the
-- digest table populates. These UPDATEs affect statements executed AFTER them,
-- so they precede the adversarial workload below.
UPDATE performance_schema.setup_consumers SET ENABLED = 'YES'
 WHERE NAME IN ('statements_digest', 'events_statements_current', 'events_statements_history');
UPDATE performance_schema.setup_instruments SET ENABLED = 'YES', TIMED = 'YES'
 WHERE NAME LIKE 'statement/%';

-- ============================================================================
-- #647 structural-query adversarial workload (specifications/structural-query-text.md,
-- INV-SQT-01 / TC-SQT-08). These statements EXECUTE so the server populates
-- performance_schema.events_statements_summary_by_digest with NORMALIZED
-- DIGEST_TEXT (literals -> ?). The live verifier (Test*LiveStructuralQueryText)
-- asserts the exported DIGEST_TEXT contains '?' placeholders and NONE of the
-- literal tokens below. These tokens are the single source of truth, mirrored
-- by structuralAdversarialLiterals in internal/collector/structural_query_qual.go.
--   string:    sqtlit_str_6f1a2b
--   integer:   918273645
--   decimal:   61728.39455
--   date/ts:   2023-09-14 / 2023-09-14 10:11:12
--   IN list:   771001, 771002, 771003
--   LIKE:      sqtlit_like_7c9
-- Identifiers (region, accounts, email, the sqt_alias alias) MAY appear in
-- DIGEST_TEXT — that is expected structural evidence, not a leak.
-- ============================================================================
SELECT id FROM accounts WHERE region = 'sqtlit_str_6f1a2b' AND balance = 61728.39455;
SELECT id FROM ledger WHERE account_id = 918273645;
SELECT id FROM replay_fixture WHERE created_at > '2023-09-14 10:11:12' AND created_at < '2023-09-14';
SELECT id FROM ledger WHERE id IN (771001, 771002, 771003);
SELECT id FROM replay_fixture WHERE email LIKE 'sqtlit_like_7c9%';
SELECT /* sqtlit_str_6f1a2b comment */ COUNT(*) AS n FROM accounts WHERE balance > 61728.39455;
SELECT a.id FROM accounts a JOIN ledger l ON l.account_id = a.id WHERE a.region = 'sqtlit_str_6f1a2b';
SELECT id FROM accounts WHERE id IN (SELECT account_id FROM ledger WHERE amount > 61728.39455);
SELECT `region` AS `sqt_alias` FROM accounts WHERE `region` = 'sqtlit_str_6f1a2b';
PREPARE sqt_stmt FROM 'SELECT id FROM accounts WHERE region = ?';
SET @sqt_r = 'sqtlit_str_6f1a2b';
EXECUTE sqt_stmt USING @sqt_r;
DEALLOCATE PREPARE sqt_stmt;
-- Long statement: normalized form exceeds the 4096-byte cap -> truncation leg.
SELECT id FROM accounts WHERE id = 0 OR id = 1 OR id = 2 OR id = 3 OR id = 4 OR id = 5 OR id = 6 OR id = 7 OR id = 8 OR id = 9 OR id = 10 OR id = 11 OR id = 12 OR id = 13 OR id = 14 OR id = 15 OR id = 16 OR id = 17 OR id = 18 OR id = 19 OR id = 20 OR id = 21 OR id = 22 OR id = 23 OR id = 24 OR id = 25 OR id = 26 OR id = 27 OR id = 28 OR id = 29 OR id = 30 OR id = 31 OR id = 32 OR id = 33 OR id = 34 OR id = 35 OR id = 36 OR id = 37 OR id = 38 OR id = 39 OR id = 40 OR id = 41 OR id = 42 OR id = 43 OR id = 44 OR id = 45 OR id = 46 OR id = 47 OR id = 48 OR id = 49 OR id = 50 OR id = 51 OR id = 52 OR id = 53 OR id = 54 OR id = 55 OR id = 56 OR id = 57 OR id = 58 OR id = 59 OR id = 60 OR id = 61 OR id = 62 OR id = 63 OR id = 64 OR id = 65 OR id = 66 OR id = 67 OR id = 68 OR id = 69 OR id = 70 OR id = 71 OR id = 72 OR id = 73 OR id = 74 OR id = 75 OR id = 76 OR id = 77 OR id = 78 OR id = 79 OR id = 80 OR id = 81 OR id = 82 OR id = 83 OR id = 84 OR id = 85 OR id = 86 OR id = 87 OR id = 88 OR id = 89 OR id = 90 OR id = 91 OR id = 92 OR id = 93 OR id = 94 OR id = 95 OR id = 96 OR id = 97 OR id = 98 OR id = 99 OR id = 100 OR id = 101 OR id = 102 OR id = 103 OR id = 104 OR id = 105 OR id = 106 OR id = 107 OR id = 108 OR id = 109 OR id = 110 OR id = 111 OR id = 112 OR id = 113 OR id = 114 OR id = 115 OR id = 116 OR id = 117 OR id = 118 OR id = 119 OR id = 120 OR id = 121 OR id = 122 OR id = 123 OR id = 124 OR id = 125 OR id = 126 OR id = 127 OR id = 128 OR id = 129 OR id = 130 OR id = 131 OR id = 132 OR id = 133 OR id = 134 OR id = 135 OR id = 136 OR id = 137 OR id = 138 OR id = 139 OR id = 140 OR id = 141 OR id = 142 OR id = 143 OR id = 144 OR id = 145 OR id = 146 OR id = 147 OR id = 148 OR id = 149 OR id = 150 OR id = 151 OR id = 152 OR id = 153 OR id = 154 OR id = 155 OR id = 156 OR id = 157 OR id = 158 OR id = 159 OR id = 160 OR id = 161 OR id = 162 OR id = 163 OR id = 164 OR id = 165 OR id = 166 OR id = 167 OR id = 168 OR id = 169 OR id = 170 OR id = 171 OR id = 172 OR id = 173 OR id = 174 OR id = 175 OR id = 176 OR id = 177 OR id = 178 OR id = 179 OR id = 180 OR id = 181 OR id = 182 OR id = 183 OR id = 184 OR id = 185 OR id = 186 OR id = 187 OR id = 188 OR id = 189 OR id = 190 OR id = 191 OR id = 192 OR id = 193 OR id = 194 OR id = 195 OR id = 196 OR id = 197 OR id = 198 OR id = 199 OR id = 200 OR id = 201 OR id = 202 OR id = 203 OR id = 204 OR id = 205 OR id = 206 OR id = 207 OR id = 208 OR id = 209 OR id = 210 OR id = 211 OR id = 212 OR id = 213 OR id = 214 OR id = 215 OR id = 216 OR id = 217 OR id = 218 OR id = 219 OR id = 220 OR id = 221 OR id = 222 OR id = 223 OR id = 224 OR id = 225 OR id = 226 OR id = 227 OR id = 228 OR id = 229 OR id = 230 OR id = 231 OR id = 232 OR id = 233 OR id = 234 OR id = 235 OR id = 236 OR id = 237 OR id = 238 OR id = 239 OR id = 240 OR id = 241 OR id = 242 OR id = 243 OR id = 244 OR id = 245 OR id = 246 OR id = 247 OR id = 248 OR id = 249 OR id = 250 OR id = 251 OR id = 252 OR id = 253 OR id = 254 OR id = 255 OR id = 256 OR id = 257 OR id = 258 OR id = 259 OR id = 260 OR id = 261 OR id = 262 OR id = 263 OR id = 264 OR id = 265 OR id = 266 OR id = 267 OR id = 268 OR id = 269 OR id = 270 OR id = 271 OR id = 272 OR id = 273 OR id = 274 OR id = 275 OR id = 276 OR id = 277 OR id = 278 OR id = 279 OR id = 280 OR id = 281 OR id = 282 OR id = 283 OR id = 284 OR id = 285 OR id = 286 OR id = 287 OR id = 288 OR id = 289 OR id = 290 OR id = 291 OR id = 292 OR id = 293 OR id = 294 OR id = 295 OR id = 296 OR id = 297 OR id = 298 OR id = 299 OR id = 300 OR id = 301 OR id = 302 OR id = 303 OR id = 304 OR id = 305 OR id = 306 OR id = 307 OR id = 308 OR id = 309 OR id = 310 OR id = 311 OR id = 312 OR id = 313 OR id = 314 OR id = 315 OR id = 316 OR id = 317 OR id = 318 OR id = 319 OR id = 320 OR id = 321 OR id = 322 OR id = 323 OR id = 324 OR id = 325 OR id = 326 OR id = 327 OR id = 328 OR id = 329 OR id = 330 OR id = 331 OR id = 332 OR id = 333 OR id = 334 OR id = 335 OR id = 336 OR id = 337 OR id = 338 OR id = 339 OR id = 340 OR id = 341 OR id = 342 OR id = 343 OR id = 344 OR id = 345 OR id = 346 OR id = 347 OR id = 348 OR id = 349 OR id = 350 OR id = 351 OR id = 352 OR id = 353 OR id = 354 OR id = 355 OR id = 356 OR id = 357 OR id = 358 OR id = 359 OR id = 360 OR id = 361 OR id = 362 OR id = 363 OR id = 364 OR id = 365 OR id = 366 OR id = 367 OR id = 368 OR id = 369 OR id = 370 OR id = 371 OR id = 372 OR id = 373 OR id = 374 OR id = 375 OR id = 376 OR id = 377 OR id = 378 OR id = 379 OR id = 380 OR id = 381 OR id = 382 OR id = 383 OR id = 384 OR id = 385 OR id = 386 OR id = 387 OR id = 388 OR id = 389 OR id = 390 OR id = 391 OR id = 392 OR id = 393 OR id = 394 OR id = 395 OR id = 396 OR id = 397 OR id = 398 OR id = 399 OR id = 400 OR id = 401 OR id = 402 OR id = 403 OR id = 404 OR id = 405 OR id = 406 OR id = 407 OR id = 408 OR id = 409 OR id = 410 OR id = 411 OR id = 412 OR id = 413 OR id = 414 OR id = 415 OR id = 416 OR id = 417 OR id = 418 OR id = 419 OR id = 420 OR id = 421 OR id = 422 OR id = 423 OR id = 424 OR id = 425 OR id = 426 OR id = 427 OR id = 428 OR id = 429 OR id = 430 OR id = 431 OR id = 432 OR id = 433 OR id = 434 OR id = 435 OR id = 436 OR id = 437 OR id = 438 OR id = 439 OR id = 440 OR id = 441 OR id = 442 OR id = 443 OR id = 444 OR id = 445 OR id = 446 OR id = 447 OR id = 448 OR id = 449 OR id = 450 OR id = 451 OR id = 452 OR id = 453 OR id = 454 OR id = 455 OR id = 456 OR id = 457 OR id = 458 OR id = 459 OR id = 460 OR id = 461 OR id = 462 OR id = 463 OR id = 464 OR id = 465 OR id = 466 OR id = 467 OR id = 468 OR id = 469 OR id = 470 OR id = 471 OR id = 472 OR id = 473 OR id = 474 OR id = 475 OR id = 476 OR id = 477 OR id = 478 OR id = 479 OR id = 480 OR id = 481 OR id = 482 OR id = 483 OR id = 484 OR id = 485 OR id = 486 OR id = 487 OR id = 488 OR id = 489 OR id = 490 OR id = 491 OR id = 492 OR id = 493 OR id = 494 OR id = 495 OR id = 496 OR id = 497 OR id = 498 OR id = 499 OR id = 500 OR id = 501 OR id = 502 OR id = 503 OR id = 504 OR id = 505 OR id = 506 OR id = 507 OR id = 508 OR id = 509 OR id = 510 OR id = 511 OR id = 512 OR id = 513 OR id = 514 OR id = 515 OR id = 516 OR id = 517 OR id = 518 OR id = 519 OR id = 520 OR id = 521 OR id = 522 OR id = 523 OR id = 524 OR id = 525 OR id = 526 OR id = 527 OR id = 528 OR id = 529 OR id = 530 OR id = 531 OR id = 532 OR id = 533 OR id = 534 OR id = 535 OR id = 536 OR id = 537 OR id = 538 OR id = 539 OR id = 540 OR id = 541 OR id = 542 OR id = 543 OR id = 544 OR id = 545 OR id = 546 OR id = 547 OR id = 548 OR id = 549 OR id = 550 OR id = 551 OR id = 552 OR id = 553 OR id = 554 OR id = 555 OR id = 556 OR id = 557 OR id = 558 OR id = 559 OR id = 560 OR id = 561 OR id = 562 OR id = 563 OR id = 564 OR id = 565 OR id = 566 OR id = 567 OR id = 568 OR id = 569 OR id = 570 OR id = 571 OR id = 572 OR id = 573 OR id = 574 OR id = 575 OR id = 576 OR id = 577 OR id = 578 OR id = 579 OR id = 580 OR id = 581 OR id = 582 OR id = 583 OR id = 584 OR id = 585 OR id = 586 OR id = 587 OR id = 588 OR id = 589 OR id = 590 OR id = 591 OR id = 592 OR id = 593 OR id = 594 OR id = 595 OR id = 596 OR id = 597 OR id = 598 OR id = 599 OR id = 600;

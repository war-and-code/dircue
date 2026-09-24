package com.example;

import org.springframework.web.bind.annotation.*;
import org.springframework.http.ResponseEntity;
import java.util.Collection;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * Minimal Spring MVC REST controller fixture for OWASP Noir oracle testing.
 */
@RestController
@RequestMapping("/api")
public class ItemController {

    private final Map<Integer, Item> items = new ConcurrentHashMap<>();
    private final AtomicInteger nextId = new AtomicInteger(1);

    @GetMapping("/items")
    public Collection<Item> listItems() {
        return items.values();
    }

    @PostMapping("/items")
    public ResponseEntity<Item> createItem(@RequestBody Item item) {
        int id = nextId.getAndIncrement();
        item.setId(id);
        items.put(id, item);
        return ResponseEntity.status(201).body(item);
    }

    @GetMapping("/items/{id}")
    public ResponseEntity<Item> getItem(@PathVariable int id) {
        Item item = items.get(id);
        if (item == null) {
            return ResponseEntity.notFound().build();
        }
        return ResponseEntity.ok(item);
    }

    @DeleteMapping("/items/{id}")
    public ResponseEntity<Void> deleteItem(@PathVariable int id) {
        items.remove(id);
        return ResponseEntity.noContent().build();
    }

    @GetMapping("/health")
    public Map<String, String> health() {
        return Map.of("status", "ok");
    }

    public static class Item {
        private int id;
        private String name;

        public int getId() { return id; }
        public void setId(int id) { this.id = id; }
        public String getName() { return name; }
        public void setName(String name) { this.name = name; }
    }
}

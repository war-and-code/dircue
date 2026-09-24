// Minimal Express application fixture for OWASP Noir and SARIF oracle testing.
const express = require('express');

const app = express();
app.use(express.json());

const items = {};
let nextId = 1;

// List all items.
app.get('/api/items', (req, res) => {
    res.json(Object.values(items));
});

// Create a new item.
app.post('/api/items', (req, res) => {
    const id = nextId++;
    items[id] = { id, name: req.body.name || '' };
    res.status(201).json(items[id]);
});

// Get a specific item.
app.get('/api/items/:id', (req, res) => {
    const item = items[parseInt(req.params.id, 10)];
    if (!item) {
        return res.status(404).json({ error: 'not found' });
    }
    res.json(item);
});

// Delete an item.
app.delete('/api/items/:id', (req, res) => {
    delete items[parseInt(req.params.id, 10)];
    res.status(204).send();
});

// Health check.
app.get('/health', (req, res) => {
    res.json({ status: 'ok' });
});

const PORT = process.env.PORT || 3000;
app.listen(PORT);

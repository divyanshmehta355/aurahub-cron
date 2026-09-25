import mongoose from 'mongoose';

let isConnected = false;

export async function connectDB() {
  if (isConnected && mongoose.connection.readyState === 1) {
    return mongoose.connection;
  }

  const mongoUri = process.env.MONGO_URI;
  if (!mongoUri) {
    throw new Error('MONGO_URI is not set in environment variables.');
  }

  try {
    const conn = await mongoose.connect(mongoUri, {
      bufferCommands: false,
    });
    isConnected = true;
    console.log('[Database] Connected to Aurahub MongoDB:', conn.connection.host);
    return conn.connection;
  } catch (err) {
    console.error('[Database] Connection failed:', err.message);
    throw err;
  }
}

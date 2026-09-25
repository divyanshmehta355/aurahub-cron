import mongoose from 'mongoose';

const videoSchema = new mongoose.Schema(
  {
    title: { type: String, required: true },
    fileId: { type: String, required: true, unique: true },
    thumbnailUrl: { type: String },
    category: { type: String, default: 'Other' },
    visibility: { type: String, enum: ['public', 'unlisted', 'private'], default: 'public' },
    uploader: { type: mongoose.Schema.Types.ObjectId, ref: 'User', required: true },
    isShort: { type: Boolean, default: false },
    views: { type: Number, default: 0 },
    lastRefreshedAt: { type: Date, default: undefined },
    pendingRemoteUploadId: { type: String, default: undefined },
    streamtapeStatus: { type: String, enum: ['active', 'warning', 'dead'], default: 'active' },
  },
  { timestamps: true }
);

const Video = mongoose.models.Video || mongoose.model('Video', videoSchema);
export default Video;

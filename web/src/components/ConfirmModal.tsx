import React from 'react';
import { AlertTriangle } from 'lucide-react';
import { Modal } from './Modal';

interface ConfirmModalProps {
  isOpen: boolean;
  onClose: () => void;
  onConfirm: () => void;
  title: string;
  message: string;
  confirmText?: string;
  cancelText?: string;
  variant?: 'danger' | 'warning';
}

export function ConfirmModal({
  isOpen,
  onClose,
  onConfirm,
  title,
  message,
  confirmText = '确认',
  cancelText = '取消',
  variant = 'danger'
}: ConfirmModalProps) {
  const handleConfirm = () => {
    onConfirm();
    onClose();
  };

  return (
    <Modal isOpen={isOpen} onClose={onClose} title={title}>
      <div className="space-y-4">
        <div className="flex items-start gap-3">
          <div className={`flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-full ${
            variant === 'danger' ? 'bg-red-50' : 'bg-amber-50'
          }`}>
            <AlertTriangle className={variant === 'danger' ? 'text-red-600' : 'text-amber-600'} size={20} />
          </div>
          <p className="flex-1 text-sm text-slate-600 leading-relaxed pt-1.5">{message}</p>
        </div>

        <div className="flex justify-end gap-2 pt-2">
          <button
            onClick={onClose}
            className="inline-flex min-h-10 items-center justify-center gap-2 rounded-xl border border-slate-200 bg-white px-4 py-2 text-sm font-semibold text-slate-700 shadow-sm transition-all hover:border-slate-300 hover:bg-slate-50 hover:text-slate-900 active:scale-[0.98]"
          >
            {cancelText}
          </button>
          <button
            onClick={handleConfirm}
            className={`inline-flex min-h-10 items-center justify-center gap-2 rounded-xl px-4 py-2 text-sm font-semibold text-white shadow-md transition-all hover:shadow-lg active:scale-[0.98] ${
              variant === 'danger'
                ? 'bg-red-600 hover:bg-red-700 shadow-red-600/10'
                : 'bg-amber-600 hover:bg-amber-700 shadow-amber-600/10'
            }`}
          >
            {confirmText}
          </button>
        </div>
      </div>
    </Modal>
  );
}
